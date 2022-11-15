package js

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/yaml"
	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/process"
	"github.com/dop251/goja_nodejs/require"
)

const (
	// nameModule is the name of the top-level object injected into the VM
	nameModule = "module"

	// nameExports is the key/name of the object expected to be assigned within
	// the top-level module, e.g. `module.exports = { hello: "world" }`
	nameExports = "exports"
)

// EvalJS takes JavaScript code (loaded from file or stdin etc) and returns
// a YAML serialization of the exported value (e.g. a YAML Pipeline).
// The name arg is the name of the file/stream/source of the JavaScript code,
// used for stack/error messages.
func EvalJS(name string, input []byte, log logger.Logger) ([]byte, error) {
	runtime, rootModule, err := newJavaScriptRuntime(log)
	if err != nil {
		return nil, err
	}

	// Run the script; capture the return value as a fallback in case the
	// preferred module.exports wasn't assigned.
	returnValue, err := runtime.RunScript(name, string(input))
	if err != nil {
		if exception, ok := err.(*goja.Exception); ok {
			if exception.Value().String() == "GoError: Invalid module" {
				log.Info("Use --debug to trace require() load attempts")
			}
			// log the exception and multi-line stack trace
			log.Error("%s", exception.String())
		}
		return nil, err
	}

	// Get the module.exports value assigned by the script
	value := rootModule.Get(nameExports)
	if value == nil {
		// if module.exports wasn't assigned, try the return value of the script
		value = returnValue

		if value == nil {
			return nil, errors.New("Script neither assigned module.exports nor returned a value")
		}
	}
	result := value.Export()

	// Rather than returning the interface{} from goja.Value.Export(), we'll
	// serialize it to YAML here.
	pipelineYAML, err := yaml.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("Serializing JavaScript result to YAML: %w", err)
	}

	return pipelineYAML, nil
}

// newJavaScriptRuntime builds and configures a goja.Runtime, with various
// modules loaded, and custom require() source loading.
func newJavaScriptRuntime(log logger.Logger) (*goja.Runtime, *goja.Object, error) {
	runtime := goja.New()

	// Add support for require() CommonJS modules.
	// require("buildkite/*") is handled by embedded resources/node_modules/buildkite/* filesystem.
	// Other paths are loaded from the host filesystem.
	registry := require.NewRegistry(
		require.WithLoader(requireSourceLoader(log)),
	)

	// Add basic utilities
	enableRequireModule(runtime, registry, log) // require(); must be enabled before console, process
	console.Enable(runtime)                     // console.log()
	process.Enable(runtime)                     // process.env

	// provide plugin() as a native module (implemented in Go)
	// This is implemented natively as a proof-of-concept; there's no good reason
	// for this to be implemented in Go rather than an embedded .js file.
	registry.RegisterNativeModule("buildkite/plugin", pluginNativeModule)

	// provide assignable module.exports for Pipeline result
	rootModule := runtime.NewObject()
	err := runtime.Set(nameModule, rootModule)
	if err != nil {
		return nil, nil, err
	}

	return runtime, rootModule, nil
}

// embeddedFS embeds node_modules from the source tree into the compiled binary
// as a virtual filesystem, which requireSourceLoader accesses.
//
//go:embed node_modules
var embeddedFS embed.FS

// requireSourceLoader is a require.SourceLoader which loads
// require("buildkite/*") from a filesystem embedded in the compiled binary,
// and delegates other paths to require.DefaultSourceLoader to be loaded from
// the host filesystem.
func requireSourceLoader(log logger.Logger) require.SourceLoader {
	return func(name string) ([]byte, error) {
		// attempt to load require("buildkite/*") from embedded FS,
		// but continue to default filesystem loader when not found.
		if strings.HasPrefix(name, "node_modules/buildkite/") {
			data, err := embeddedFS.ReadFile(name)
			if errors.Is(err, fs.ErrNotExist) {
				log.Debug("  loader=embedded %q %v", name, require.ModuleFileDoesNotExistError)
				// continue to default loader
			} else if err != nil {
				log.Debug("  loader=embedded %q %v", name, err)
				return nil, err
			} else {
				log.Debug("  loader=embedded %q loaded %d bytes", name, len(data))
				return data, nil
			}
		}

		data, err := require.DefaultSourceLoader(name)
		if err != nil {
			log.Debug("  loader=default %q %v", name, err)
			return data, err
		}
		log.Debug("  loader=default %q loaded %d bytes", name, len(data))
		return data, err
	}
}

// pluginNativeModule implements a basic `plugin(name, ver, config)` JS function,
// as a proof of concept of native modules. It should really be implemented as
// an embedded JavaScript file, but this demonstrates how to implement native
// functions that interact with Go code in more complex ways.
func pluginNativeModule(runtime *goja.Runtime, module *goja.Object) {
	module.Set("exports", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0)
		ref := call.Argument(1)
		config := call.Argument(2)
		plugin := runtime.NewObject()
		plugin.Set(name.String()+"#"+ref.String(), config)
		return plugin
	})
}

// enableRequireModule adds goja_nodejs's require() function to the runtime,
// wrapped in some custom debug logging and error reporting.
func enableRequireModule(runtime *goja.Runtime, registry *require.Registry, log logger.Logger) {
	// enable goja_nodejs's require()
	registry.Enable(runtime)

	// get a reference to goja_nodejs's require()
	orig, ok := goja.AssertFunction(runtime.Get("require"))
	if !ok {
		panic("expected `require` to be a function")
	}

	// a stack of names being recursively loaded
	var stack []string

	// wrap require() to log/track the name being required
	runtime.Set("require", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0)

		// track this name on our stack
		stack = append(stack, name.String())
		defer func() { stack = stack[:len(stack)-1] }()

		log.Debug("require(%q) [%s]", name, strings.Join(stack, " → "))

		// call the original goja_nodejs require()
		res, err := orig(goja.Undefined(), name)
		if err != nil {
			if exception, ok := err.(*goja.Exception); ok {
				if exception.Value().String() == "GoError: Invalid module" {
					// report the head of the require() name stack
					log.Error("require(%q)", stack[len(stack)-1])
				}
			}
			// propagate the error to goja.Runtime
			panic(err)
		}

		log.Debug("  require(%q) finished", name)
		return res
	})
}
