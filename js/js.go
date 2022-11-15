package js

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"

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
func EvalJS(name string, input []byte) ([]byte, error) {
	runtime, rootModule, err := newJavaScriptRuntime()
	if err != nil {
		return nil, err
	}

	// Run the script; we don't need to capture the return value of the script,
	// we'll access module.exports instead.
	returnValue, err := runtime.RunScript(name, string(input))
	if err != nil {
		return nil, err
	}

	// Get the module.exports valus assigned by the script
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

func newJavaScriptRuntime() (*goja.Runtime, *goja.Object, error) {
	runtime := goja.New()

	// Add support for require() CommonJS modules.
	// require("buildkite/*") is handled by embedded resources/node_modules/buildkite/* filesystem.
	// Other paths are loaded from the host filesystem.
	registry := require.NewRegistry(
		require.WithLoader(requireSourceLoader),
	)

	// Add basic utilities
	registry.Enable(runtime) // require(); must be enabled before console, process
	console.Enable(runtime)  // console.log()
	process.Enable(runtime)  // process.env

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

// FS is an embedded filesystem.
//
//go:embed node_modules
var embeddedFS embed.FS

// requireSourceLoader is a require.SourceLoader which loads
// require("buildkite/*") from a filesystem embedded in the compiled binary,
// and delegates other paths to require.DefaultSourceLoader to be loaded from
// the host filesystem.
func requireSourceLoader(name string) ([]byte, error) {
	if !strings.HasPrefix(name, "node_modules/buildkite/") {
		return require.DefaultSourceLoader(name)
	}
	data, err := embeddedFS.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, require.ModuleFileDoesNotExistError
	} else if err != nil {
		return nil, err
	}
	return data, nil
}

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
