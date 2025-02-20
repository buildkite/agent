package clicommand

import (
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
	{Config: AnnotateConfig{}, Command: AnnotateCommand},
	{Config: AnnotationRemoveConfig{}, Command: AnnotationRemoveCommand},
	{Config: ArtifactDownloadConfig{}, Command: ArtifactDownloadCommand},
	{Config: ArtifactSearchConfig{}, Command: ArtifactSearchCommand},
	{Config: ArtifactShasumConfig{}, Command: ArtifactShasumCommand},
	{Config: ArtifactUploadConfig{}, Command: ArtifactUploadCommand},
	{Config: BuildCancelConfig{}, Command: BuildCancelCommand},
	{Config: BootstrapConfig{}, Command: BootstrapCommand},
	{Config: EnvDumpConfig{}, Command: EnvDumpCommand},
	{Config: EnvGetConfig{}, Command: EnvGetCommand},
	{Config: EnvSetConfig{}, Command: EnvSetCommand},
	{Config: EnvUnsetConfig{}, Command: EnvUnsetCommand},
	{Config: GitCredentialsHelperConfig{}, Command: GitCredentialsHelperCommand},
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

		fields, err := reflections.Fields(pair.Config)
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
