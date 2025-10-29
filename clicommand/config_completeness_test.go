package clicommand

import (
	"fmt"
	"strings"
	"testing"

	"github.com/oleiade/reflections"
	"github.com/urfave/cli"
)

type configCommandPair struct {
	Config  any
	Command cli.Command
}

var commandConfigPairs = []configCommandPair{
	{Config: AcknowledgementsConfig{}, Command: AcknowledgementsCommand},
	{Config: AgentStartConfig{}, Command: AgentStartCommand},
	{Config: AgentStopConfig{}, Command: AgentStopCommand},
	{Config: AgentPauseConfig{}, Command: AgentPauseCommand},
	{Config: AgentResumeConfig{}, Command: AgentResumeCommand},
	{Config: AnnotateConfig{}, Command: AnnotateCommand},
	{Config: AnnotationRemoveConfig{}, Command: AnnotationRemoveCommand},
	{Config: ArtifactDownloadConfig{}, Command: ArtifactDownloadCommand},
	{Config: ArtifactSearchConfig{}, Command: ArtifactSearchCommand},
	{Config: ArtifactShasumConfig{}, Command: ArtifactShasumCommand},
	{Config: ArtifactUploadConfig{}, Command: ArtifactUploadCommand},
	{Config: BuildCancelConfig{}, Command: BuildCancelCommand},
	{Config: BootstrapConfig{}, Command: BootstrapCommand},
	{Config: CacheRestoreConfig{}, Command: CacheRestoreCommand},
	{Config: CacheSaveConfig{}, Command: CacheSaveCommand},
	{Config: EnvDumpConfig{}, Command: EnvDumpCommand},
	{Config: EnvGetConfig{}, Command: EnvGetCommand},
	{Config: EnvSetConfig{}, Command: EnvSetCommand},
	{Config: EnvUnsetConfig{}, Command: EnvUnsetCommand},
	{Config: GitCredentialsHelperConfig{}, Command: GitCredentialsHelperCommand},
	{Config: KubernetesBootstrapConfig{}, Command: KubernetesBootstrapCommand},
	{Config: LockAcquireConfig{}, Command: LockAcquireCommand},
	{Config: LockDoConfig{}, Command: LockDoCommand},
	{Config: LockDoneConfig{}, Command: LockDoneCommand},
	{Config: LockGetConfig{}, Command: LockGetCommand},
	{Config: LockReleaseConfig{}, Command: LockReleaseCommand},
	{Config: MetaDataExistsConfig{}, Command: MetaDataExistsCommand},
	{Config: MetaDataGetConfig{}, Command: MetaDataGetCommand},
	{Config: MetaDataKeysConfig{}, Command: MetaDataKeysCommand},
	{Config: MetaDataSetConfig{}, Command: MetaDataSetCommand},
	{Config: OIDCTokenConfig{}, Command: OIDCRequestTokenCommand},
	{Config: PipelineUploadConfig{}, Command: PipelineUploadCommand},
	{Config: RedactorAddConfig{}, Command: RedactorAddCommand},
	{Config: SecretGetConfig{}, Command: SecretGetCommand},
	{Config: StepCancelConfig{}, Command: StepCancelCommand},
	{Config: StepGetConfig{}, Command: StepGetCommand},
	{Config: StepUpdateConfig{}, Command: StepUpdateCommand},
	{Config: ToolKeygenConfig{}, Command: ToolKeygenCommand},
	{Config: ToolSignConfig{}, Command: ToolSignCommand},
}

func TestAllCommandConfigStructsHaveCorrespondingCLIFlags(t *testing.T) {
	t.Parallel()

	for _, pair := range commandConfigPairs {
		flagNames := make(map[string]struct{}, len(pair.Command.Flags))
		for _, flag := range pair.Command.Flags {
			flagNames[flag.GetName()] = struct{}{}
		}

		fields, err := reflections.FieldsDeep(pair.Config)
		if err != nil {
			t.Fatalf("getting fields for type %T: %v", pair.Config, err)
		}

		cliStructTags := make(map[string]struct{}, len(fields))
		for _, field := range fields {
			cliName, err := reflections.GetFieldTag(pair.Config, field, "cli")
			if err != nil {
				t.Fatalf("getting cli tag for field %s of %T: %v", pair.Config, field, err)
			}

			if strings.HasPrefix(cliName, "arg:") {
				continue
			}

			cliStructTags[cliName] = struct{}{}

			if _, ok := flagNames[cliName]; !ok {
				t.Errorf("field %s of %T has cli tag %s, but no corresponding CLI flag", field, pair.Config, cliName)
			}
		}

		for tag := range flagNames {
			if _, ok := cliStructTags[tag]; !ok {
				t.Errorf("CLI flag %s has no corresponding field in %T", tag, pair.Config)
			}
		}
	}
}

func TestDescriptionsAreIndentedUsingSpaces(t *testing.T) {
	t.Parallel()

	for name, command := range commandsByFullName(t, BuildkiteAgentCommands) {
		if command.Description == "" {
			t.Fatalf("command %q has no description; please add one", name)
		}

		lines := strings.Split(command.Description, "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, "\t") {
				fullCommandName := "buildkite-agent " + name
				t.Errorf("line %d of description for command %q contains tab characters; please use spaces for indentation in command descriptions", i, fullCommandName)
			}
		}
	}
}

// cli.Command.FullName() doesn't actually print the full name of a command when its a subcommand,
// so we need to build a map of full command names to cli.Command structs ourselves
func commandsByFullName(t *testing.T, commands []cli.Command) map[string]cli.Command {
	t.Helper()

	result := make(map[string]cli.Command)

	for _, command := range commands {
		if len(command.Subcommands) == 0 {
			result[command.FullName()] = command
		}

		for _, subcommand := range command.Subcommands {
			subcommands := commandsByFullName(t, []cli.Command{subcommand})
			for subcommandName, cmd := range subcommands {
				result[fmt.Sprintf("%s %s", command.FullName(), subcommandName)] = cmd
			}
		}
	}

	return result
}

func TestAllCommandsAreTestedForConfigCompleteness(t *testing.T) {
	t.Parallel()

	allCommands := make([]cli.Command, 0, len(commandConfigPairs))
	for _, command := range BuildkiteAgentCommands {
		if len(command.Subcommands) > 0 {
			allCommands = append(allCommands, command.Subcommands...)
		} else {
			allCommands = append(allCommands, command)
		}
	}

	for _, command := range allCommands {
		found := false
		for _, pair := range commandConfigPairs {
			if pair.Command.FullName() == command.FullName() {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("command %q is not being tested for config completeness in config_completeness_test.go\n Add it and its associated config struct to the commandConfigPairs slice in config_completeness_test.go", command.FullName())
		}
	}
}
