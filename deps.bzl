load("@gazelle//:deps.bzl", "go_repository")

def go_dependencies():
    go_repository(
        name = "com_github_99designs_gqlgen",
        importpath = "github.com/99designs/gqlgen",
        sum = "h1:OS2wLk/67Y+vXM75XHbwRnNYJcbuJd4OBL76RX3NQQA=",
        version = "v0.17.44",
    )
    go_repository(
        name = "com_github_agnivade_levenshtein",
        importpath = "github.com/agnivade/levenshtein",
        sum = "h1:QY8M92nrzkmr798gCo3kmMyqXFzdQVpxLlGPRBij0P8=",
        version = "v1.1.1",
    )
    go_repository(
        name = "com_github_alexflint_go_arg",
        importpath = "github.com/alexflint/go-arg",
        sum = "h1:lDWZAXxpAnZUq4qwb86p/3rIJJ2Li81EoMbTMujhVa0=",
        version = "v1.4.2",
    )
    go_repository(
        name = "com_github_alexflint_go_scalar",
        importpath = "github.com/alexflint/go-scalar",
        sum = "h1:NGupf1XV/Xb04wXskDFzS0KWOLH632W/EO4fAFi+A70=",
        version = "v1.0.0",
    )
    go_repository(
        name = "com_github_andreyvit_diff",
        importpath = "github.com/andreyvit/diff",
        sum = "h1:bvNMNQO63//z+xNgfBlViaCIJKLlCJ6/fmUseuG0wVQ=",
        version = "v0.0.0-20170406064948-c7f18ee00883",
    )
    go_repository(
        name = "com_github_andybalholm_brotli",
        importpath = "github.com/andybalholm/brotli",
        sum = "h1:Yf9fFpf49Zrxb9NlQaluyE92/+X7UVHlhMNJN2sxfOI=",
        version = "v1.0.6",
    )
    go_repository(
        name = "com_github_anmitsu_go_shlex",
        importpath = "github.com/anmitsu/go-shlex",
        sum = "h1:9AeTilPcZAjCFIImctFaOjnTIavg87rW78vTPkQqLI8=",
        version = "v0.0.0-20200514113438-38f4b401e2be",
    )
    go_repository(
        name = "com_github_antihax_optional",
        importpath = "github.com/antihax/optional",
        sum = "h1:xK2lYat7ZLaVVcIuj82J8kIro4V6kDe0AUDFboUCwcg=",
        version = "v1.0.0",
    )
    go_repository(
        name = "com_github_arbovm_levenshtein",
        importpath = "github.com/arbovm/levenshtein",
        sum = "h1:jfIu9sQUG6Ig+0+Ap1h4unLjW6YQJpKZVmUzxsD4E/Q=",
        version = "v0.0.0-20160628152529-48b4e1c0c4d0",
    )
    go_repository(
        name = "com_github_armon_go_metrics",
        importpath = "github.com/armon/go-metrics",
        sum = "h1:hR91U9KYmb6bLBYLQjyM+3j+rcd/UhE+G78SFnF8gJA=",
        version = "v0.4.1",
    )
    go_repository(
        name = "com_github_armon_go_radix",
        importpath = "github.com/armon/go-radix",
        sum = "h1:BUAU3CGlLvorLI26FmByPp2eC2qla6E1Tw+scpcg/to=",
        version = "v0.0.0-20180808171621-7fddfc383310",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go",
        importpath = "github.com/aws/aws-sdk-go",
        sum = "h1:KKUZBfBoyqy5d3swXyiC7Q76ic40rYcbqH7qjh59kzU=",
        version = "v1.55.5",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2",
        importpath = "github.com/aws/aws-sdk-go-v2",
        sum = "h1:7BokKRgRPuGmKkFMhEg/jSul+tB9VvXhcViILtfG8b4=",
        version = "v1.32.6",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_aws_protocol_eventstream",
        importpath = "github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream",
        sum = "h1:OPLEkmhXf6xFPiz0bLeDArZIDx1NNS4oJyG4nv3Gct0=",
        version = "v1.4.13",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_config",
        importpath = "github.com/aws/aws-sdk-go-v2/config",
        sum = "h1:D89IKtGrs/I3QXOLNTH93NJYtDhm8SYa9Q5CsPShmyo=",
        version = "v1.28.6",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_credentials",
        importpath = "github.com/aws/aws-sdk-go-v2/credentials",
        sum = "h1:48bA+3/fCdi2yAwVt+3COvmatZ6jUDNkDTIsqDiMUdw=",
        version = "v1.17.47",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_feature_ec2_imds",
        importpath = "github.com/aws/aws-sdk-go-v2/feature/ec2/imds",
        sum = "h1:AmoU1pziydclFT/xRV+xXE/Vb8fttJCLRPv8oAkprc0=",
        version = "v1.16.21",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_internal_configsources",
        importpath = "github.com/aws/aws-sdk-go-v2/internal/configsources",
        sum = "h1:s/fF4+yDQDoElYhfIVvSNyeCydfbuTKzhxSXDXCPasU=",
        version = "v1.3.25",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_internal_endpoints_v2",
        importpath = "github.com/aws/aws-sdk-go-v2/internal/endpoints/v2",
        sum = "h1:ZntTCl5EsYnhN/IygQEUugpdwbhdkom9uHcbCftiGgA=",
        version = "v2.6.25",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_internal_ini",
        importpath = "github.com/aws/aws-sdk-go-v2/internal/ini",
        sum = "h1:VaRN3TlFdd6KxX1x3ILT5ynH6HvKgqdiXoTxAF4HQcQ=",
        version = "v1.8.1",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_internal_v4a",
        importpath = "github.com/aws/aws-sdk-go-v2/internal/v4a",
        sum = "h1:uHhWcrNBgpm9gi3o8NSQcsAqha/U9OFYzi2k4+0UVz8=",
        version = "v1.1.3",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_dynamodb",
        importpath = "github.com/aws/aws-sdk-go-v2/service/dynamodb",
        sum = "h1:x3V1JRHq7q9RUbDpaeNpLH7QoipGpCo3fdnMMuSeABU=",
        version = "v1.21.4",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_ec2",
        importpath = "github.com/aws/aws-sdk-go-v2/service/ec2",
        sum = "h1:c6a19AjfhEXKlEX63cnlWtSQ4nzENihHZOG0I3wH6BE=",
        version = "v1.93.2",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_eventbridge",
        importpath = "github.com/aws/aws-sdk-go-v2/service/eventbridge",
        sum = "h1:G18wotYZxZ0A5tkqKv6FHCjsF86UQrqNHy5LS+T7JWM=",
        version = "v1.20.4",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_internal_accept_encoding",
        importpath = "github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding",
        sum = "h1:iXtILhvDxB6kPvEXgsDhGaZCSC6LQET5ZHSdJozeI0Y=",
        version = "v1.12.1",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_internal_checksum",
        importpath = "github.com/aws/aws-sdk-go-v2/service/internal/checksum",
        sum = "h1:oCUrlTzh9GwhlYdyDGNAS6UgqJRzJp5rKoYCJWqLyZI=",
        version = "v1.1.35",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_internal_endpoint_discovery",
        importpath = "github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery",
        sum = "h1:JlxVMFDHivlhNOIxd2O/9z4O0wC2zIC4lRB71lejVHU=",
        version = "v1.7.34",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_internal_presigned_url",
        importpath = "github.com/aws/aws-sdk-go-v2/service/internal/presigned-url",
        sum = "h1:50+XsN70RS7dwJ2CkVNXzj7U2L1HKP8nqTd3XWEXBN4=",
        version = "v1.12.6",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_internal_s3shared",
        importpath = "github.com/aws/aws-sdk-go-v2/service/internal/s3shared",
        sum = "h1:rPDAISw3FjEhrJoaxmQjuD+GgBfv2p3AVhmAcnyqq3k=",
        version = "v1.15.3",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_kinesis",
        importpath = "github.com/aws/aws-sdk-go-v2/service/kinesis",
        sum = "h1:UohaQds+Puk9BEbvncXkZduIGYImxohbFpVmSoymXck=",
        version = "v1.18.4",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_kms",
        importpath = "github.com/aws/aws-sdk-go-v2/service/kms",
        sum = "h1:dZmNIRtPUvtvUIIDVNpvtnJQ8N8Iqm7SQAxf18htZYw=",
        version = "v1.37.7",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_s3",
        importpath = "github.com/aws/aws-sdk-go-v2/service/s3",
        sum = "h1:NAc8WQsVQ3+kz3rU619mlz8NcbpZI6FVJHQfH33QK0g=",
        version = "v1.32.0",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_sfn",
        importpath = "github.com/aws/aws-sdk-go-v2/service/sfn",
        sum = "h1:yIyFY2kbCOoHvuivf9minqnP2RLYJgmvQRYxakIb2oI=",
        version = "v1.19.4",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_sns",
        importpath = "github.com/aws/aws-sdk-go-v2/service/sns",
        sum = "h1:Asj098jPfIZYzAbk4xVFwVBGij5hgMcli0d+5Pe4aZA=",
        version = "v1.21.4",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_sqs",
        importpath = "github.com/aws/aws-sdk-go-v2/service/sqs",
        sum = "h1:bp8KUUx15mnLMe8SSJqO/kYEn0C2kKfWq/M9SRK9i1E=",
        version = "v1.24.4",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_sso",
        importpath = "github.com/aws/aws-sdk-go-v2/service/sso",
        sum = "h1:rLnYAfXQ3YAccocshIH5mzNNwZBkBo+bP6EhIxak6Hw=",
        version = "v1.24.7",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_ssooidc",
        importpath = "github.com/aws/aws-sdk-go-v2/service/ssooidc",
        sum = "h1:JnhTZR3PiYDNKlXy50/pNeix9aGMo6lLpXwJ1mw8MD4=",
        version = "v1.28.6",
    )
    go_repository(
        name = "com_github_aws_aws_sdk_go_v2_service_sts",
        importpath = "github.com/aws/aws-sdk-go-v2/service/sts",
        sum = "h1:s4074ZO1Hk8qv65GqNXqDjmkf4HSQqJukaLuuW0TpDA=",
        version = "v1.33.2",
    )
    go_repository(
        name = "com_github_aws_smithy_go",
        importpath = "github.com/aws/smithy-go",
        sum = "h1:/HPHZQ0g7f4eUeK6HKglFz8uwVfZKgoI25rb/J+dnro=",
        version = "v1.22.1",
    )
    go_repository(
        name = "com_github_azure_azure_sdk_for_go_sdk_azcore",
        importpath = "github.com/Azure/azure-sdk-for-go/sdk/azcore",
        sum = "h1:JZg6HRh6W6U4OLl6lk7BZ7BLisIzM9dG1R50zUk9C/M=",
        version = "v1.16.0",
    )
    go_repository(
        name = "com_github_azure_azure_sdk_for_go_sdk_azidentity",
        importpath = "github.com/Azure/azure-sdk-for-go/sdk/azidentity",
        sum = "h1:B/dfvscEQtew9dVuoxqxrUKKv8Ih2f55PydknDamU+g=",
        version = "v1.8.0",
    )
    go_repository(
        name = "com_github_azure_azure_sdk_for_go_sdk_azidentity_cache",
        importpath = "github.com/Azure/azure-sdk-for-go/sdk/azidentity/cache",
        sum = "h1:+m0M/LFxN43KvULkDNfdXOgrjtg6UYJPFBJyuEcRCAw=",
        version = "v0.3.0",
    )
    go_repository(
        name = "com_github_azure_azure_sdk_for_go_sdk_internal",
        importpath = "github.com/Azure/azure-sdk-for-go/sdk/internal",
        sum = "h1:ywEEhmNahHBihViHepv3xPBn1663uRv2t2q/ESv9seY=",
        version = "v1.10.0",
    )
    go_repository(
        name = "com_github_azure_azure_sdk_for_go_sdk_resourcemanager_storage_armstorage",
        importpath = "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage",
        sum = "h1:PiSrjRPpkQNjrM8H0WwKMnZUdu1RGMtd/LdGKUrOo+c=",
        version = "v1.6.0",
    )
    go_repository(
        name = "com_github_azure_azure_sdk_for_go_sdk_storage_azblob",
        importpath = "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob",
        sum = "h1:mlmW46Q0B79I+Aj4azKC6xDMFN9a9SyZWESlGWYXbFs=",
        version = "v1.5.0",
    )
    go_repository(
        name = "com_github_azuread_microsoft_authentication_extensions_for_go_cache",
        importpath = "github.com/AzureAD/microsoft-authentication-extensions-for-go/cache",
        sum = "h1:WJTmL004Abzc5wDB5VtZG2PJk5ndYDgVacGqfirKxjM=",
        version = "v0.1.1",
    )
    go_repository(
        name = "com_github_azuread_microsoft_authentication_library_for_go",
        importpath = "github.com/AzureAD/microsoft-authentication-library-for-go",
        sum = "h1:XHOnouVk1mxXfQidrMEnLlPk9UMeRtyBTnEFtxkV0kU=",
        version = "v1.2.2",
    )
    go_repository(
        name = "com_github_beorn7_perks",
        importpath = "github.com/beorn7/perks",
        sum = "h1:VlbKKnNfV8bJzeqoa4cOKqO6bYr3WgKZxO8Z16+hsOM=",
        version = "v1.0.1",
    )
    go_repository(
        name = "com_github_bgentry_speakeasy",
        importpath = "github.com/bgentry/speakeasy",
        sum = "h1:ByYyxL9InA1OWqxJqqp2A5pYHUrCiAL6K3J+LKSsQkY=",
        version = "v0.1.0",
    )
    go_repository(
        name = "com_github_bradfitz_gomemcache",
        importpath = "github.com/bradfitz/gomemcache",
        sum = "h1:Dr+ezPI5ivhMn/3WOoB86XzMhie146DNaBbhaQWZHMY=",
        version = "v0.0.0-20230611145640-acc696258285",
    )
    go_repository(
        name = "com_github_bradleyjkemp_cupaloy_v2",
        importpath = "github.com/bradleyjkemp/cupaloy/v2",
        sum = "h1:knToPYa2xtfg42U3I6punFEjaGFKWQRXJwj0JTv4mTs=",
        version = "v2.6.0",
    )
    go_repository(
        name = "com_github_brunoscheufler_aws_ecs_metadata_go",
        importpath = "github.com/brunoscheufler/aws-ecs-metadata-go",
        sum = "h1:WCnJxXZXx9c8gwz598wvdqmu+YTzB9wx2X1OovK3Le8=",
        version = "v0.0.0-20220812150832-b6b31c6eeeaf",
    )
    go_repository(
        name = "com_github_buildkite_bintest_v3",
        importpath = "github.com/buildkite/bintest/v3",
        sum = "h1:RTWcSaJRlOT6t/K311ejPf+0J3LE/QEODzVG3vlLnWo=",
        version = "v3.3.0",
    )
    go_repository(
        name = "com_github_buildkite_go_pipeline",
        importpath = "github.com/buildkite/go-pipeline",
        sum = "h1:llI7sAdZ7sqYE7r8ePlmDADRhJ1K0Kua2+gv74Z9+Es=",
        version = "v0.13.3",
    )
    go_repository(
        name = "com_github_buildkite_interpolate",
        importpath = "github.com/buildkite/interpolate",
        sum = "h1:v2Ji3voik69UZlbfoqzx+qfcsOKLA61nHdU79VV+tPU=",
        version = "v0.1.5",
    )
    go_repository(
        name = "com_github_buildkite_roko",
        importpath = "github.com/buildkite/roko",
        sum = "h1:hbNURz//dQqNl6Eo9awjQOVOZwSDJ8VEbBDxSfT9rGQ=",
        version = "v1.2.0",
    )
    go_repository(
        name = "com_github_buildkite_shellwords",
        importpath = "github.com/buildkite/shellwords",
        sum = "h1:hiVSLk7s3yFKFOHF/huoShLqrj13RMguWX2yzfvy7es=",
        version = "v0.0.0-20180315084142-c3f497d1e000",
    )
    go_repository(
        name = "com_github_burntsushi_toml",
        importpath = "github.com/BurntSushi/toml",
        sum = "h1:kuoIxZQy2WRRk1pttg9asf+WVv6tWQuBNVmK8+nqPr0=",
        version = "v1.4.0",
    )
    go_repository(
        name = "com_github_bytedance_sonic",
        importpath = "github.com/bytedance/sonic",
        sum = "h1:qtNZduETEIWJVIyDl01BeNxur2rW9OwTQ/yBqFRkKEk=",
        version = "v1.10.0",
    )
    go_repository(
        name = "com_github_cenkalti_backoff_v3",
        importpath = "github.com/cenkalti/backoff/v3",
        sum = "h1:cfUAAO3yvKMYKPrvhDuHSwQnhZNk/RMHKdZqKTxfm6M=",
        version = "v3.2.2",
    )
    go_repository(
        name = "com_github_cenkalti_backoff_v4",
        importpath = "github.com/cenkalti/backoff/v4",
        sum = "h1:MyRJ/UdXutAwSAT+s3wNd7MfTIcy71VQueUuFK343L8=",
        version = "v4.3.0",
    )
    go_repository(
        name = "com_github_census_instrumentation_opencensus_proto",
        importpath = "github.com/census-instrumentation/opencensus-proto",
        sum = "h1:iKLQ0xPNFxR/2hzXZMrBo8f1j86j5WHzznCCQxV/b8g=",
        version = "v0.4.1",
    )
    go_repository(
        name = "com_github_cespare_xxhash_v2",
        importpath = "github.com/cespare/xxhash/v2",
        sum = "h1:UL815xU9SqsFlibzuggzjXhog7bL6oX9BbNZnL2UFvs=",
        version = "v2.3.0",
    )
    go_repository(
        name = "com_github_chenzhuoyu_base64x",
        importpath = "github.com/chenzhuoyu/base64x",
        sum = "h1:77cEq6EriyTZ0g/qfRdp61a3Uu/AWrgIq2s0ClJV1g0=",
        version = "v0.0.0-20230717121745-296ad89f973d",
    )
    go_repository(
        name = "com_github_chenzhuoyu_iasm",
        importpath = "github.com/chenzhuoyu/iasm",
        sum = "h1:9fhXjVzq5hUy2gkhhgHl95zG2cEAhw9OSGs8toWWAwo=",
        version = "v0.9.0",
    )
    go_repository(
        name = "com_github_cihub_seelog",
        importpath = "github.com/cihub/seelog",
        sum = "h1:kHaBemcxl8o/pQ5VM1c8PVE1PubbNx3mjUr09OqWGCs=",
        version = "v0.0.0-20170130134532-f561c5e57575",
    )
    go_repository(
        name = "com_github_cncf_xds_go",
        importpath = "github.com/cncf/xds/go",
        sum = "h1:QVw89YDxXxEe+l8gU8ETbOasdwEV+avkR75ZzsVV9WI=",
        version = "v0.0.0-20240905190251-b4127c9b8d78",
    )
    go_repository(
        name = "com_github_codahale_rfc6979",
        importpath = "github.com/codahale/rfc6979",
        sum = "h1:EDmT6Q9Zs+SbUoc7Ik9EfrFqcylYqgPZ9ANSbTAntnE=",
        version = "v0.0.0-20141003034818-6a90f24967eb",
    )
    go_repository(
        name = "com_github_confluentinc_confluent_kafka_go",
        importpath = "github.com/confluentinc/confluent-kafka-go",
        sum = "h1:gV/GxhMBUb03tFWkN+7kdhg+zf+QUM+wVkI9zwh770Q=",
        version = "v1.9.2",
    )
    go_repository(
        name = "com_github_confluentinc_confluent_kafka_go_v2",
        importpath = "github.com/confluentinc/confluent-kafka-go/v2",
        sum = "h1:qy+SfqDauR/TX2qH2VuZqA1rcEAqApBYtHpI6rcqM0U=",
        version = "v2.2.0",
    )
    go_repository(
        name = "com_github_containerd_cgroups_v3",
        importpath = "github.com/containerd/cgroups/v3",
        sum = "h1:f5WFqIVSgo5IZmtTT3qVBo6TzI1ON6sycSBKkymb9L0=",
        version = "v3.0.2",
    )
    go_repository(
        name = "com_github_coreos_go_systemd_v22",
        importpath = "github.com/coreos/go-systemd/v22",
        sum = "h1:RrqgGjYQKalulkV8NGVIfkXQf6YYmOyiJKk8iXXhfZs=",
        version = "v22.5.0",
    )
    go_repository(
        name = "com_github_cpuguy83_go_md2man_v2",
        importpath = "github.com/cpuguy83/go-md2man/v2",
        sum = "h1:ZtcqGrnekaHpVLArFSe4HK5DoKx1T0rq2DwVB0alcyc=",
        version = "v2.0.5",
    )
    go_repository(
        name = "com_github_creack_pty",
        importpath = "github.com/creack/pty",
        sum = "h1:tUN6H7LWqNx4hQVxomd0CVsDwaDr9gaRQaI4GpSmrsA=",
        version = "v1.1.19",
    )
    go_repository(
        name = "com_github_datadog_appsec_internal_go",
        importpath = "github.com/DataDog/appsec-internal-go",
        sum = "h1:cGOneFsg0JTRzWl5U2+og5dbtyW3N8XaYwc5nXe39Vw=",
        version = "v1.9.0",
    )
    go_repository(
        name = "com_github_datadog_datadog_agent_comp_trace_compression_def",
        importpath = "github.com/DataDog/datadog-agent/comp/trace/compression/def",
        sum = "h1:/ZlaAi23xGLqkiMugEROC/T99PfC/VUKYAeZy3MtKcU=",
        version = "v0.58.0",
    )
    go_repository(
        name = "com_github_datadog_datadog_agent_comp_trace_compression_impl_gzip",
        importpath = "github.com/DataDog/datadog-agent/comp/trace/compression/impl-gzip",
        sum = "h1:VVUjGWu56G3oL8tENSheg3QvnTYS2mUVf6hK3oQMM/Y=",
        version = "v0.58.0",
    )
    go_repository(
        name = "com_github_datadog_datadog_agent_comp_trace_compression_impl_zstd",
        importpath = "github.com/DataDog/datadog-agent/comp/trace/compression/impl-zstd",
        sum = "h1:cC+aNB6sn9lqVQiKL9nDicjcpvdjOSmBShWzrNE3eco=",
        version = "v0.58.0",
    )
    go_repository(
        name = "com_github_datadog_datadog_agent_pkg_obfuscate",
        importpath = "github.com/DataDog/datadog-agent/pkg/obfuscate",
        sum = "h1:nOrRNCHyriM/EjptMrttFOQhRSmvfagESdpyknb5VPg=",
        version = "v0.58.0",
    )
    go_repository(
        name = "com_github_datadog_datadog_agent_pkg_proto",
        importpath = "github.com/DataDog/datadog-agent/pkg/proto",
        sum = "h1:JX2Q0C5QnKcYqnYHWUcP0z7R0WB8iiQz3aWn+kT5DEc=",
        version = "v0.58.0",
    )
    go_repository(
        name = "com_github_datadog_datadog_agent_pkg_remoteconfig_state",
        importpath = "github.com/DataDog/datadog-agent/pkg/remoteconfig/state",
        sum = "h1:5hGO0Z8ih0bRojuq+1ZwLFtdgsfO3TqIjbwJAH12sOQ=",
        version = "v0.58.0",
    )
    go_repository(
        name = "com_github_datadog_datadog_agent_pkg_trace",
        importpath = "github.com/DataDog/datadog-agent/pkg/trace",
        sum = "h1:4AjohoBWWN0nNaeD/0SDZ8lRTYmnJ48CqREevUfSets=",
        version = "v0.58.0",
    )
    go_repository(
        name = "com_github_datadog_datadog_agent_pkg_util_cgroups",
        importpath = "github.com/DataDog/datadog-agent/pkg/util/cgroups",
        sum = "h1:aeRv6FqP2LBFfT3k8DJeJ2Y3ZXgK02LjswhYSgY3Vao=",
        version = "v0.58.0",
    )
    go_repository(
        name = "com_github_datadog_datadog_agent_pkg_util_log",
        importpath = "github.com/DataDog/datadog-agent/pkg/util/log",
        sum = "h1:2MENBnHNw2Vx/ebKRyOPMqvzWOUps2Ol2o/j8uMvN4U=",
        version = "v0.58.0",
    )
    go_repository(
        name = "com_github_datadog_datadog_agent_pkg_util_pointer",
        importpath = "github.com/DataDog/datadog-agent/pkg/util/pointer",
        sum = "h1:UNjHnu9k66f6Ehi/MXKRktgHTxFYUVuMN0oO51fBeAU=",
        version = "v0.58.0",
    )
    go_repository(
        name = "com_github_datadog_datadog_agent_pkg_util_scrubber",
        importpath = "github.com/DataDog/datadog-agent/pkg/util/scrubber",
        sum = "h1:Jkf91q3tuIer4Hv9CLJIYjlmcelAsoJRMmkHyz+p1Dc=",
        version = "v0.58.0",
    )
    go_repository(
        name = "com_github_datadog_datadog_go_v5",
        importpath = "github.com/DataDog/datadog-go/v5",
        sum = "h1:2oCLxjF/4htd55piM75baflj/KoE6VYS7alEUqFvRDw=",
        version = "v5.6.0",
    )
    go_repository(
        name = "com_github_datadog_go_libddwaf_v3",
        importpath = "github.com/DataDog/go-libddwaf/v3",
        sum = "h1:GWA4ln4DlLxiXm+X7HA/oj0ZLcdCwOS81KQitegRTyY=",
        version = "v3.5.1",
    )
    go_repository(
        name = "com_github_datadog_go_runtime_metrics_internal",
        importpath = "github.com/DataDog/go-runtime-metrics-internal",
        sum = "h1:s4hgS6gqbXIakEMMujYiHCVVsB3R3oZtqEzPBMnFU2w=",
        version = "v0.0.0-20241106155157-194426bbbd59",
    )
    go_repository(
        name = "com_github_datadog_go_sqllexer",
        importpath = "github.com/DataDog/go-sqllexer",
        sum = "h1:xUQh2tLr/95LGxDzLmttLgTo/1gzFeOyuwrQa/Iig4Q=",
        version = "v0.0.14",
    )
    go_repository(
        name = "com_github_datadog_go_tuf",
        importpath = "github.com/DataDog/go-tuf",
        sum = "h1:4CagiIekonLSfL8GMHRHcHudo1fQnxELS9g4tiAupQ4=",
        version = "v1.1.0-0.5.2",
    )
    go_repository(
        name = "com_github_datadog_gostackparse",
        importpath = "github.com/DataDog/gostackparse",
        sum = "h1:i7dLkXHvYzHV308hnkvVGDL3BR4FWl7IsXNPz/IGQh4=",
        version = "v0.7.0",
    )
    go_repository(
        name = "com_github_datadog_opentelemetry_mapping_go_pkg_otlp_attributes",
        importpath = "github.com/DataDog/opentelemetry-mapping-go/pkg/otlp/attributes",
        sum = "h1:fKv05WFWHCXQmUTehW1eEZvXJP65Qv00W4V01B1EqSA=",
        version = "v0.20.0",
    )
    go_repository(
        name = "com_github_datadog_sketches_go",
        build_directives = [
            "gazelle:resolve go github.com/DataDog/sketches-go/ddsketch/pb/sketchpb //ddsketch/pb/sketchpb",  # keep
        ],
        build_file_generation = "off",
        importpath = "github.com/DataDog/sketches-go",
        build_file_proto_mode = "disable",
        sum = "h1:ki7VfeNz7IcNafq7yI/j5U/YCkO3LJiMDtXz9OMQbyE=",
        version = "v1.4.5",
    )
    go_repository(
        name = "com_github_datadog_zstd",
        importpath = "github.com/DataDog/zstd",
        sum = "h1:oWf5W7GtOLgp6bciQYDmhHHjdhYkALu6S/5Ni9ZgSvQ=",
        version = "v1.5.5",
    )
    go_repository(
        name = "com_github_davecgh_go_spew",
        importpath = "github.com/davecgh/go-spew",
        sum = "h1:U9qPSI2PIWSS1VwoXQT9A3Wy9MM3WgvqSxFWenqJduM=",
        version = "v1.1.2-0.20180830191138-d8f796af33cc",
    )
    go_repository(
        name = "com_github_decred_dcrd_crypto_blake256",
        importpath = "github.com/decred/dcrd/crypto/blake256",
        sum = "h1:7PltbUIQB7u/FfZ39+DGa/ShuMyJ5ilcvdfma9wOH6Y=",
        version = "v1.0.1",
    )
    go_repository(
        name = "com_github_decred_dcrd_dcrec_secp256k1_v4",
        importpath = "github.com/decred/dcrd/dcrec/secp256k1/v4",
        sum = "h1:rpfIENRNNilwHwZeG5+P150SMrnNEcHYvcCuK6dPZSg=",
        version = "v4.3.0",
    )
    go_repository(
        name = "com_github_denisbrodbeck_machineid",
        importpath = "github.com/denisbrodbeck/machineid",
        sum = "h1:geKr9qtkB876mXguW2X6TU4ZynleN6ezuMSRhl4D7AQ=",
        version = "v1.0.1",
    )
    go_repository(
        name = "com_github_denisenkom_go_mssqldb",
        importpath = "github.com/denisenkom/go-mssqldb",
        sum = "h1:9rHa233rhdOyrz2GcP9NM+gi2psgJZ4GWDpL/7ND8HI=",
        version = "v0.11.0",
    )
    go_repository(
        name = "com_github_dgryski_go_farm",
        importpath = "github.com/dgryski/go-farm",
        sum = "h1:fAjc9m62+UWV/WAFKLNi6ZS0675eEUC9y3AlwSbQu1Y=",
        version = "v0.0.0-20200201041132-a6ae2369ad13",
    )
    go_repository(
        name = "com_github_dgryski_go_rendezvous",
        importpath = "github.com/dgryski/go-rendezvous",
        sum = "h1:lO4WD4F/rVNCu3HqELle0jiPLLBs70cWOduZpkS1E78=",
        version = "v0.0.0-20200823014737-9f7001d12a5f",
    )
    go_repository(
        name = "com_github_dgryski_trifles",
        importpath = "github.com/dgryski/trifles",
        sum = "h1:fRzb/w+pyskVMQ+UbP35JkH8yB7MYb4q/qhBarqZE6g=",
        version = "v0.0.0-20200323201526-dd97f9abfb48",
    )
    go_repository(
        name = "com_github_dimfeld_httptreemux_v5",
        importpath = "github.com/dimfeld/httptreemux/v5",
        sum = "h1:p8jkiMrCuZ0CmhwYLcbNbl7DDo21fozhKHQ2PccwOFQ=",
        version = "v5.5.0",
    )
    go_repository(
        name = "com_github_docker_go_units",
        importpath = "github.com/docker/go-units",
        sum = "h1:69rxXcBk27SvSaaxTtLh/8llcHD8vYHT7WSdRZ/jvr4=",
        version = "v0.5.0",
    )
    go_repository(
        name = "com_github_dustin_go_humanize",
        importpath = "github.com/dustin/go-humanize",
        sum = "h1:GzkhY7T5VNhEkwH0PVJgjz+fX1rhBrR7pRT3mDkpeCY=",
        version = "v1.0.1",
    )
    go_repository(
        name = "com_github_dustinkirkland_golang_petname",
        importpath = "github.com/dustinkirkland/golang-petname",
        sum = "h1:S6Dco8FtAhEI/qkg/00H6RdEGC+MCy5GPiQ+xweNRFE=",
        version = "v0.0.0-20231002161417-6a283f1aaaf2",
    )
    go_repository(
        name = "com_github_eapache_go_resiliency",
        importpath = "github.com/eapache/go-resiliency",
        sum = "h1:3OK9bWpPk5q6pbFAaYSEwD9CLUSHG8bnZuqX2yMt3B0=",
        version = "v1.4.0",
    )
    go_repository(
        name = "com_github_eapache_go_xerial_snappy",
        importpath = "github.com/eapache/go-xerial-snappy",
        sum = "h1:Oy0F4ALJ04o5Qqpdz8XLIpNA3WM/iSIXqxtqo7UGVws=",
        version = "v0.0.0-20230731223053-c322873962e3",
    )
    go_repository(
        name = "com_github_eapache_queue",
        importpath = "github.com/eapache/queue",
        sum = "h1:YOEu7KNc61ntiQlcEeUIoDTJ2o8mQznoNvUhiigpIqc=",
        version = "v1.1.0",
    )
    go_repository(
        name = "com_github_eapache_queue_v2",
        importpath = "github.com/eapache/queue/v2",
        sum = "h1:8EXxF+tCLqaVk8AOC29zl2mnhQjwyLxxOTuhUazWRsg=",
        version = "v2.0.0-20230407133247-75960ed334e4",
    )
    go_repository(
        name = "com_github_ebitengine_purego",
        importpath = "github.com/ebitengine/purego",
        sum = "h1:EYID3JOAdmQ4SNZYJHu9V6IqOeRQDBYxqKAg9PyoHFY=",
        version = "v0.6.0-alpha.5",
    )
    go_repository(
        name = "com_github_elastic_elastic_transport_go_v8",
        importpath = "github.com/elastic/elastic-transport-go/v8",
        sum = "h1:NeqEz1ty4RQz+TVbUrpSU7pZ48XkzGWQj02k5koahIE=",
        version = "v8.1.0",
    )
    go_repository(
        name = "com_github_elastic_go_elasticsearch_v6",
        importpath = "github.com/elastic/go-elasticsearch/v6",
        sum = "h1:U2HtkBseC1FNBmDr0TR2tKltL6FxoY+niDAlj5M8TK8=",
        version = "v6.8.5",
    )
    go_repository(
        name = "com_github_elastic_go_elasticsearch_v7",
        importpath = "github.com/elastic/go-elasticsearch/v7",
        sum = "h1:49mHcHx7lpCL8cW1aioEwSEVKQF3s+Igi4Ye/QTWwmk=",
        version = "v7.17.1",
    )
    go_repository(
        name = "com_github_elastic_go_elasticsearch_v8",
        importpath = "github.com/elastic/go-elasticsearch/v8",
        sum = "h1:Rn1mcqaIMcNT43hnx2H62cIFZ+B6mjWtzj85BDKrvCE=",
        version = "v8.4.0",
    )
    go_repository(
        name = "com_github_emicklei_go_restful",
        importpath = "github.com/emicklei/go-restful",
        sum = "h1:rgqiKNjTnFQA6kkhFe16D8epTksy9HQ1MyrbDXSdYhM=",
        version = "v2.16.0+incompatible",
    )
    go_repository(
        name = "com_github_emicklei_go_restful_v3",
        importpath = "github.com/emicklei/go-restful/v3",
        sum = "h1:rAQeMHw1c7zTmncogyy8VvRZwtkmkZ4FxERmMY4rD+g=",
        version = "v3.11.0",
    )
    go_repository(
        name = "com_github_envoyproxy_go_control_plane",
        importpath = "github.com/envoyproxy/go-control-plane",
        sum = "h1:HzkeUz1Knt+3bK+8LG1bxOO/jzWZmdxpwC51i202les=",
        version = "v0.13.0",
    )
    go_repository(
        name = "com_github_envoyproxy_protoc_gen_validate",
        importpath = "github.com/envoyproxy/protoc-gen-validate",
        sum = "h1:tntQDh69XqOCOZsDz0lVJQez/2L6Uu2PdjCQwWCJ3bM=",
        version = "v1.1.0",
    )
    go_repository(
        name = "com_github_fatih_color",
        importpath = "github.com/fatih/color",
        sum = "h1:zmkK9Ngbjj+K0yRhTVONQh1p/HknKYSlNT+vZCzyokM=",
        version = "v1.16.0",
    )
    go_repository(
        name = "com_github_felixge_httpsnoop",
        importpath = "github.com/felixge/httpsnoop",
        sum = "h1:NFTV2Zj1bL4mc9sqWACXbQFVBBg2W3GPvqp8/ESS2Wg=",
        version = "v1.0.4",
    )
    go_repository(
        name = "com_github_flynn_go_docopt",
        importpath = "github.com/flynn/go-docopt",
        sum = "h1:Ss/B3/5wWRh8+emnK0++g5zQzwDTi30W10pKxKc4JXI=",
        version = "v0.0.0-20140912013429-f6dd2ebbb31e",
    )
    go_repository(
        name = "com_github_fortytw2_leaktest",
        importpath = "github.com/fortytw2/leaktest",
        sum = "h1:u8491cBMTQ8ft8aeV+adlcytMZylmA5nnwwkRZjI8vw=",
        version = "v1.3.0",
    )
    go_repository(
        name = "com_github_gabriel_vasile_mimetype",
        importpath = "github.com/gabriel-vasile/mimetype",
        sum = "h1:w5qFW6JKBz9Y393Y4q372O9A7cUSequkh1Q7OhCmWKU=",
        version = "v1.4.2",
    )
    go_repository(
        name = "com_github_garyburd_redigo",
        importpath = "github.com/garyburd/redigo",
        sum = "h1:LFu2R3+ZOPgSMWMOL+saa/zXRjw0ID2G8FepO53BGlg=",
        version = "v1.6.4",
    )
    go_repository(
        name = "com_github_gin_contrib_sse",
        importpath = "github.com/gin-contrib/sse",
        sum = "h1:Y/yl/+YNO8GZSjAhjMsSuLt29uWRFHdHYUb5lYOV9qE=",
        version = "v0.1.0",
    )
    go_repository(
        name = "com_github_gin_gonic_gin",
        importpath = "github.com/gin-gonic/gin",
        sum = "h1:4idEAncQnU5cB7BeOkPtxjfCSye0AAm1R0RVIqJ+Jmg=",
        version = "v1.9.1",
    )
    go_repository(
        name = "com_github_gliderlabs_ssh",
        importpath = "github.com/gliderlabs/ssh",
        sum = "h1:a4YXD1V7xMF9g5nTkdfnja3Sxy1PVDCj1Zg4Wb8vY6c=",
        version = "v0.3.8",
    )
    go_repository(
        name = "com_github_globalsign_mgo",
        importpath = "github.com/globalsign/mgo",
        sum = "h1:DujepqpGd1hyOd7aW59XpK7Qymp8iy83xq74fLr21is=",
        version = "v0.0.0-20181015135952-eeefdecb41b8",
    )
    go_repository(
        name = "com_github_go_chi_chi",
        importpath = "github.com/go-chi/chi",
        sum = "h1:QHdzF2szwjqVV4wmByUnTcsbIg7UGaQ0tPF2t5GcAIs=",
        version = "v1.5.4",
    )
    go_repository(
        name = "com_github_go_chi_chi_v5",
        importpath = "github.com/go-chi/chi/v5",
        sum = "h1:Aj1EtB0qR2Rdo2dG4O94RIU35w2lvQSj6BRA4+qwFL0=",
        version = "v5.2.0",
    )
    go_repository(
        name = "com_github_go_jose_go_jose_v3",
        importpath = "github.com/go-jose/go-jose/v3",
        sum = "h1:fFKWeig/irsp7XD2zBxvnmA/XaRWp5V3CBsZXJF7G7k=",
        version = "v3.0.3",
    )
    go_repository(
        name = "com_github_go_logr_logr",
        importpath = "github.com/go-logr/logr",
        sum = "h1:6pFjapn8bFcIbiKo3XT4j/BhANplGihG6tvd+8rYgrY=",
        version = "v1.4.2",
    )
    go_repository(
        name = "com_github_go_logr_stdr",
        importpath = "github.com/go-logr/stdr",
        sum = "h1:hSWxHoqTgW2S2qGc0LTAI563KZ5YKYRhT3MFKZMbjag=",
        version = "v1.2.2",
    )
    go_repository(
        name = "com_github_go_ole_go_ole",
        importpath = "github.com/go-ole/go-ole",
        sum = "h1:/Fpf6oFPoeFik9ty7siob0G6Ke8QvQEuVcuChpwXzpY=",
        version = "v1.2.6",
    )
    go_repository(
        name = "com_github_go_openapi_jsonpointer",
        importpath = "github.com/go-openapi/jsonpointer",
        sum = "h1:gZr+CIYByUqjcgeLXnQu2gHYQC9o73G2XUeOFYEICuY=",
        version = "v0.19.5",
    )
    go_repository(
        name = "com_github_go_openapi_jsonreference",
        importpath = "github.com/go-openapi/jsonreference",
        sum = "h1:1WJP/wi4OjB4iV8KVbH73rQaoialJrqv8gitZLxGLtM=",
        version = "v0.19.5",
    )
    go_repository(
        name = "com_github_go_openapi_swag",
        importpath = "github.com/go-openapi/swag",
        sum = "h1:gm3vOOXfiuw5i9p5N9xJvfjvuofpyvLA9Wr6QfK5Fng=",
        version = "v0.19.14",
    )
    go_repository(
        name = "com_github_go_pg_pg_v10",
        importpath = "github.com/go-pg/pg/v10",
        sum = "h1:vYwbFpqoMpTDphnzIPshPPepdy3VpzD8qo29OFKp4vo=",
        version = "v10.11.1",
    )
    go_repository(
        name = "com_github_go_pg_zerochecker",
        importpath = "github.com/go-pg/zerochecker",
        sum = "h1:pp7f72c3DobMWOb2ErtZsnrPaSvHd2W4o9//8HtF4mU=",
        version = "v0.2.0",
    )
    go_repository(
        name = "com_github_go_playground_locales",
        importpath = "github.com/go-playground/locales",
        sum = "h1:EWaQ/wswjilfKLTECiXz7Rh+3BjFhfDFKv/oXslEjJA=",
        version = "v0.14.1",
    )
    go_repository(
        name = "com_github_go_playground_universal_translator",
        importpath = "github.com/go-playground/universal-translator",
        sum = "h1:Bcnm0ZwsGyWbCzImXv+pAJnYK9S473LQFuzCbDbfSFY=",
        version = "v0.18.1",
    )
    go_repository(
        name = "com_github_go_playground_validator_v10",
        importpath = "github.com/go-playground/validator/v10",
        sum = "h1:BSe8uhN+xQ4r5guV/ywQI4gO59C2raYcGffYWZEjZzM=",
        version = "v10.15.1",
    )
    go_repository(
        name = "com_github_go_redis_redis",
        importpath = "github.com/go-redis/redis",
        sum = "h1:K0pv1D7EQUjfyoMql+r/jZqCLizCGKFlFgcHWWmHQjg=",
        version = "v6.15.9+incompatible",
    )
    go_repository(
        name = "com_github_go_redis_redis_v7",
        importpath = "github.com/go-redis/redis/v7",
        sum = "h1:PASvf36gyUpr2zdOUS/9Zqc80GbM+9BDyiJSJDDOrTI=",
        version = "v7.4.1",
    )
    go_repository(
        name = "com_github_go_redis_redis_v8",
        importpath = "github.com/go-redis/redis/v8",
        sum = "h1:AcZZR7igkdvfVmQTPnu9WE37LRrO/YrBH5zWyjDC0oI=",
        version = "v8.11.5",
    )
    go_repository(
        name = "com_github_go_sql_driver_mysql",
        importpath = "github.com/go-sql-driver/mysql",
        sum = "h1:BCTh4TKNUYmOmMUcQ3IipzF5prigylS7XXjEkfCHuOE=",
        version = "v1.6.0",
    )
    go_repository(
        name = "com_github_go_viper_mapstructure_v2",
        importpath = "github.com/go-viper/mapstructure/v2",
        sum = "h1:TQcrn6Wq+sKGkpyPvppOz99zsMBaUOKXq6HSv655U1c=",
        version = "v2.0.0-alpha.1",
    )
    go_repository(
        name = "com_github_goccy_go_json",
        importpath = "github.com/goccy/go-json",
        sum = "h1:KZ5WoDbxAIgm2HNbYckL0se1fHD6rz5j4ywS6ebzDqA=",
        version = "v0.10.3",
    )
    go_repository(
        name = "com_github_gocql_gocql",
        importpath = "github.com/gocql/gocql",
        sum = "h1:IdFdOTbnpbd0pDhl4REKQDM+Q0SzKXQ1Yh+YZZ8T/qU=",
        version = "v1.6.0",
    )
    go_repository(
        name = "com_github_godbus_dbus_v5",
        importpath = "github.com/godbus/dbus/v5",
        sum = "h1:mkgN1ofwASrYnJ5W6U/BxG15eXXXjirgZc7CLqkcaro=",
        version = "v5.0.6",
    )
    go_repository(
        name = "com_github_gofiber_fiber_v2",
        importpath = "github.com/gofiber/fiber/v2",
        sum = "h1:tWoP1MJQjGEe4GB5TUGOi7P2E0ZMMRx5ZTG4rT+yGMo=",
        version = "v2.52.5",
    )
    go_repository(
        name = "com_github_gofrs_flock",
        importpath = "github.com/gofrs/flock",
        sum = "h1:MTLVXXHf8ekldpJk3AKicLij9MdwOWkZ+a/jHHZby9E=",
        version = "v0.12.1",
    )
    go_repository(
        name = "com_github_gofrs_uuid",
        importpath = "github.com/gofrs/uuid",
        sum = "h1:3qXRTX8/NbyulANqlc0lchS1gqAVxRgsuW1YrTJupqA=",
        version = "v4.4.0+incompatible",
    )
    go_repository(
        name = "com_github_gogo_protobuf",
        importpath = "github.com/gogo/protobuf",
        sum = "h1:Ov1cvc58UF3b5XjBnZv7+opcTcQFZebYjWzi34vdm4Q=",
        version = "v1.3.2",
    )
    go_repository(
        name = "com_github_golang_glog",
        importpath = "github.com/golang/glog",
        sum = "h1:1+mZ9upx1Dh6FmUTFR1naJ77miKiXgALjWOZ3NVFPmY=",
        version = "v1.2.2",
    )
    go_repository(
        name = "com_github_golang_groupcache",
        importpath = "github.com/golang/groupcache",
        sum = "h1:oI5xCqsCo564l8iNU+DwB5epxmsaqB+rhGL0m5jtYqE=",
        version = "v0.0.0-20210331224755-41bb18bfe9da",
    )
    go_repository(
        name = "com_github_golang_jwt_jwt_v5",
        importpath = "github.com/golang-jwt/jwt/v5",
        sum = "h1:OuVbFODueb089Lh128TAcimifWaLhJwVflnrgM17wHk=",
        version = "v5.2.1",
    )
    go_repository(
        name = "com_github_golang_mock",
        importpath = "github.com/golang/mock",
        sum = "h1:ErTB+efbowRARo13NNdxyJji2egdxLGQhRaY+DUumQc=",
        version = "v1.6.0",
    )
    go_repository(
        name = "com_github_golang_protobuf",
        importpath = "github.com/golang/protobuf",
        sum = "h1:i7eJL8qZTpSEXOPTxNKhASYpMn+8e5Q6AdndVa1dWek=",
        version = "v1.5.4",
    )
    go_repository(
        name = "com_github_golang_snappy",
        importpath = "github.com/golang/snappy",
        sum = "h1:yAGX7huGHXlcLOEtBnF4w7FQwA26wojNCwOYAEhLjQM=",
        version = "v0.0.4",
    )
    go_repository(
        name = "com_github_golang_sql_civil",
        importpath = "github.com/golang-sql/civil",
        sum = "h1:au07oEsX2xN0ktxqI+Sida1w446QrXBRJ0nee3SNZlA=",
        version = "v0.0.0-20220223132316-b832511892a9",
    )
    go_repository(
        name = "com_github_golang_sql_sqlexp",
        importpath = "github.com/golang-sql/sqlexp",
        sum = "h1:ZCD6MBpcuOVfGVqsEmY5/4FtYiKz6tSyUv9LPEDei6A=",
        version = "v0.1.0",
    )
    go_repository(
        name = "com_github_gomodule_redigo",
        importpath = "github.com/gomodule/redigo",
        sum = "h1:Sl3u+2BI/kk+VEatbj0scLdrFhjPmbxOc1myhDP41ws=",
        version = "v1.8.9",
    )
    go_repository(
        name = "com_github_google_gnostic",
        importpath = "github.com/google/gnostic",
        sum = "h1:FhTMOKj2VhjpouxvWJAV1TL304uMlb9zcDqkl6cEI54=",
        version = "v0.5.7-v3refs",
    )
    go_repository(
        name = "com_github_google_go_cmp",
        importpath = "github.com/google/go-cmp",
        sum = "h1:ofyhxvXcZhMsU5ulbFiLKl/XBFqE1GSq7atu8tAmTRI=",
        version = "v0.6.0",
    )
    go_repository(
        name = "com_github_google_go_pkcs11",
        importpath = "github.com/google/go-pkcs11",
        sum = "h1:PVRnTgtArZ3QQqTGtbtjtnIkzl2iY2kt24yqbrf7td8=",
        version = "v0.3.0",
    )
    go_repository(
        name = "com_github_google_go_querystring",
        importpath = "github.com/google/go-querystring",
        sum = "h1:AnCroh3fv4ZBgVIf1Iwtovgjaw/GiKJo8M8yD/fhyJ8=",
        version = "v1.1.0",
    )
    go_repository(
        name = "com_github_google_gofuzz",
        importpath = "github.com/google/gofuzz",
        sum = "h1:xRy4A+RhZaiKjJ1bPfwQ8sedCA+YS2YcCHW6ec7JMi0=",
        version = "v1.2.0",
    )
    go_repository(
        name = "com_github_google_pprof",
        importpath = "github.com/google/pprof",
        sum = "h1:h9U78+dx9a4BKdQkBBos92HalKpaGKHrp+3Uo6yTodo=",
        version = "v0.0.0-20230817174616-7a8ec2ada47b",
    )
    go_repository(
        name = "com_github_google_s2a_go",
        importpath = "github.com/google/s2a-go",
        sum = "h1:zZDs9gcbt9ZPLV0ndSyQk6Kacx2g/X+SKYovpnz3SMM=",
        version = "v0.1.8",
    )
    go_repository(
        name = "com_github_google_uuid",
        importpath = "github.com/google/uuid",
        sum = "h1:NIvaJDMOsjHA8n1jAhLSgzrAzy1Hgr+hNrb57e+94F0=",
        version = "v1.6.0",
    )
    go_repository(
        name = "com_github_googleapis_enterprise_certificate_proxy",
        importpath = "github.com/googleapis/enterprise-certificate-proxy",
        sum = "h1:XYIDZApgAnrN1c855gTgghdIA6Stxb52D5RnLI1SLyw=",
        version = "v0.3.4",
    )
    go_repository(
        name = "com_github_googleapis_gax_go_v2",
        importpath = "github.com/googleapis/gax-go/v2",
        sum = "h1:f+jMrjBPl+DL9nI4IQzLUxMq7XrAqFYB7hBPqMNIe8o=",
        version = "v2.14.0",
    )
    go_repository(
        name = "com_github_gorilla_mux",
        importpath = "github.com/gorilla/mux",
        sum = "h1:i40aqfkR1h2SlN9hojwV5ZA91wcXFOvkdNIeFDP5koI=",
        version = "v1.8.0",
    )
    go_repository(
        name = "com_github_gorilla_websocket",
        importpath = "github.com/gorilla/websocket",
        sum = "h1:PPwGk2jz7EePpoHN/+ClbZu8SPxiqlu12wZP/3sWmnc=",
        version = "v1.5.0",
    )
    go_repository(
        name = "com_github_gowebpki_jcs",
        importpath = "github.com/gowebpki/jcs",
        sum = "h1:Qjzg8EOkrOTuWP7DqQ1FbYtcpEbeTzUoTN9bptp8FOU=",
        version = "v1.0.1",
    )
    go_repository(
        name = "com_github_graph_gophers_graphql_go",
        importpath = "github.com/graph-gophers/graphql-go",
        sum = "h1:fDqblo50TEpD0LY7RXk/LFVYEVqo3+tXMNMPSVXA1yc=",
        version = "v1.5.0",
    )
    go_repository(
        name = "com_github_graphql_go_graphql",
        importpath = "github.com/graphql-go/graphql",
        sum = "h1:p7/Ou/WpmulocJeEx7wjQy611rtXGQaAcXGqanuMMgc=",
        version = "v0.8.1",
    )
    go_repository(
        name = "com_github_graphql_go_handler",
        importpath = "github.com/graphql-go/handler",
        sum = "h1:CANh8WPnl5M9uA25c2GBhPqJhE53Fg0Iue/fRNla71E=",
        version = "v0.2.3",
    )
    go_repository(
        name = "com_github_grpc_ecosystem_grpc_gateway",
        importpath = "github.com/grpc-ecosystem/grpc-gateway",
        sum = "h1:gmcG1KaJ57LophUzW0Hy8NmPhnMZb4M0+kPpLofRdBo=",
        version = "v1.16.0",
    )
    go_repository(
        name = "com_github_grpc_ecosystem_grpc_gateway_v2",
        importpath = "github.com/grpc-ecosystem/grpc-gateway/v2",
        sum = "h1:TmHmbvxPmaegwhDubVz0lICL0J5Ka2vwTzhoePEXsGE=",
        version = "v2.24.0",
    )
    go_repository(
        name = "com_github_hailocab_go_hostpool",
        importpath = "github.com/hailocab/go-hostpool",
        sum = "h1:5upAirOpQc1Q53c0bnx2ufif5kANL7bfZWcc6VJWJd8=",
        version = "v0.0.0-20160125115350-e80d13ce29ed",
    )
    go_repository(
        name = "com_github_hashicorp_consul_api",
        importpath = "github.com/hashicorp/consul/api",
        sum = "h1:u2XyStA2j0jnCiVUU7Qyrt8idjRn4ORhK6DlvZ3bWhA=",
        version = "v1.24.0",
    )
    go_repository(
        name = "com_github_hashicorp_errwrap",
        importpath = "github.com/hashicorp/errwrap",
        sum = "h1:OxrOeh75EUXMY8TBjag2fzXGZ40LB6IKw45YeGUDY2I=",
        version = "v1.1.0",
    )
    go_repository(
        name = "com_github_hashicorp_go_cleanhttp",
        importpath = "github.com/hashicorp/go-cleanhttp",
        sum = "h1:035FKYIWjmULyFRBKPs8TBQoi0x6d9G4xc9neXJWAZQ=",
        version = "v0.5.2",
    )
    go_repository(
        name = "com_github_hashicorp_go_hclog",
        importpath = "github.com/hashicorp/go-hclog",
        sum = "h1:Qr2kF+eVWjTiYmU7Y31tYlP1h0q/X3Nl3tPGdaB11/k=",
        version = "v1.6.3",
    )
    go_repository(
        name = "com_github_hashicorp_go_immutable_radix",
        importpath = "github.com/hashicorp/go-immutable-radix",
        sum = "h1:DKHmCUm2hRBK510BaiZlwvpD40f8bJFeZnpfm2KLowc=",
        version = "v1.3.1",
    )
    go_repository(
        name = "com_github_hashicorp_go_multierror",
        importpath = "github.com/hashicorp/go-multierror",
        sum = "h1:H5DkEtf6CXdFp0N0Em5UCwQpXMWke8IA0+lD48awMYo=",
        version = "v1.1.1",
    )
    go_repository(
        name = "com_github_hashicorp_go_retryablehttp",
        importpath = "github.com/hashicorp/go-retryablehttp",
        sum = "h1:C8hUCYzor8PIfXHa4UrZkU4VvK8o9ISHxT2Q8+VepXU=",
        version = "v0.7.7",
    )
    go_repository(
        name = "com_github_hashicorp_go_rootcerts",
        importpath = "github.com/hashicorp/go-rootcerts",
        sum = "h1:jzhAVGtqPKbwpyCPELlgNWhE1znq+qwJtW5Oi2viEzc=",
        version = "v1.0.2",
    )
    go_repository(
        name = "com_github_hashicorp_go_secure_stdlib_parseutil",
        importpath = "github.com/hashicorp/go-secure-stdlib/parseutil",
        sum = "h1:UpiO20jno/eV1eVZcxqWnUohyKRe1g8FPV/xH1s/2qs=",
        version = "v0.1.7",
    )
    go_repository(
        name = "com_github_hashicorp_go_secure_stdlib_strutil",
        importpath = "github.com/hashicorp/go-secure-stdlib/strutil",
        sum = "h1:kes8mmyCpxJsI7FTwtzRqEy9CdjCtrXrXGuOpxEA7Ts=",
        version = "v0.1.2",
    )
    go_repository(
        name = "com_github_hashicorp_go_sockaddr",
        importpath = "github.com/hashicorp/go-sockaddr",
        sum = "h1:ztczhD1jLxIRjVejw8gFomI1BQZOe2WoVOu0SyteCQc=",
        version = "v1.0.2",
    )
    go_repository(
        name = "com_github_hashicorp_go_uuid",
        importpath = "github.com/hashicorp/go-uuid",
        sum = "h1:2gKiV6YVmrJ1i2CKKa9obLvRieoRGviZFL26PcT/Co8=",
        version = "v1.0.3",
    )
    go_repository(
        name = "com_github_hashicorp_go_version",
        importpath = "github.com/hashicorp/go-version",
        sum = "h1:5tqGy27NaOTB8yJKUZELlFAS/LTKJkrmONwQKeRZfjY=",
        version = "v1.7.0",
    )
    go_repository(
        name = "com_github_hashicorp_golang_lru",
        importpath = "github.com/hashicorp/golang-lru",
        sum = "h1:dV3g9Z/unq5DpblPpw+Oqcv4dU/1omnb4Ok8iPY6p1c=",
        version = "v1.0.2",
    )
    go_repository(
        name = "com_github_hashicorp_golang_lru_v2",
        importpath = "github.com/hashicorp/golang-lru/v2",
        sum = "h1:a+bsQ5rvGLjzHuww6tVxozPZFVghXaHOwFs4luLUK2k=",
        version = "v2.0.7",
    )
    go_repository(
        name = "com_github_hashicorp_hcl",
        importpath = "github.com/hashicorp/hcl",
        sum = "h1:kI3hhbbyzr4dldA8UdTb7ZlVVlI2DACdCfz31RPDgJM=",
        version = "v1.0.1-vault-5",
    )
    go_repository(
        name = "com_github_hashicorp_serf",
        importpath = "github.com/hashicorp/serf",
        sum = "h1:Z1H2J60yRKvfDYAOZLd2MU0ND4AH/WDz7xYHDWQsIPY=",
        version = "v0.10.1",
    )
    go_repository(
        name = "com_github_hashicorp_vault_api",
        importpath = "github.com/hashicorp/vault/api",
        sum = "h1:YjkZLJ7K3inKgMZ0wzCU9OHqc+UqMQyXsPXnf3Cl2as=",
        version = "v1.9.2",
    )
    go_repository(
        name = "com_github_hashicorp_vault_sdk",
        importpath = "github.com/hashicorp/vault/sdk",
        sum = "h1:H1kitfl1rG2SHbeGEyvhEqmIjVKE3E6c2q3ViKOs6HA=",
        version = "v0.9.2",
    )
    go_repository(
        name = "com_github_ibm_sarama",
        importpath = "github.com/IBM/sarama",
        sum = "h1:QTVmX+gMKye52mT5x+Ve/Bod2D0Gy7ylE2Wslv+RHtc=",
        version = "v1.40.0",
    )
    go_repository(
        name = "com_github_imdario_mergo",
        importpath = "github.com/imdario/mergo",
        sum = "h1:b6R2BslTbIEToALKP7LxUvijTsNI9TAe80pLWN2g/HU=",
        version = "v0.3.12",
    )
    go_repository(
        name = "com_github_jackc_pgpassfile",
        importpath = "github.com/jackc/pgpassfile",
        sum = "h1:/6Hmqy13Ss2zCq62VdNG8tM1wchn8zjSGOBJ6icpsIM=",
        version = "v1.0.0",
    )
    go_repository(
        name = "com_github_jackc_pgservicefile",
        importpath = "github.com/jackc/pgservicefile",
        sum = "h1:bbPeKD0xmW/Y25WS6cokEszi5g+S0QxI/d45PkRi7Nk=",
        version = "v0.0.0-20221227161230-091c0ba34f0a",
    )
    go_repository(
        name = "com_github_jackc_pgx_v5",
        importpath = "github.com/jackc/pgx/v5",
        sum = "h1:SWJzexBzPL5jb0GEsrPMLIsi/3jOo7RHlzTjcAeDrPY=",
        version = "v5.6.0",
    )
    go_repository(
        name = "com_github_jackc_puddle_v2",
        importpath = "github.com/jackc/puddle/v2",
        sum = "h1:RhxXJtFG022u4ibrCSMSiu5aOq1i77R3OHKNJj77OAk=",
        version = "v2.2.1",
    )
    go_repository(
        name = "com_github_jcmturner_aescts_v2",
        importpath = "github.com/jcmturner/aescts/v2",
        sum = "h1:9YKLH6ey7H4eDBXW8khjYslgyqG2xZikXP0EQFKrle8=",
        version = "v2.0.0",
    )
    go_repository(
        name = "com_github_jcmturner_dnsutils_v2",
        importpath = "github.com/jcmturner/dnsutils/v2",
        sum = "h1:lltnkeZGL0wILNvrNiVCR6Ro5PGU/SeBvVO/8c/iPbo=",
        version = "v2.0.0",
    )
    go_repository(
        name = "com_github_jcmturner_gofork",
        importpath = "github.com/jcmturner/gofork",
        sum = "h1:QH0l3hzAU1tfT3rZCnW5zXl+orbkNMMRGJfdJjHVETg=",
        version = "v1.7.6",
    )
    go_repository(
        name = "com_github_jcmturner_gokrb5_v8",
        importpath = "github.com/jcmturner/gokrb5/v8",
        sum = "h1:x1Sv4HaTpepFkXbt2IkL29DXRf8sOfZXo8eRKh687T8=",
        version = "v8.4.4",
    )
    go_repository(
        name = "com_github_jcmturner_rpc_v2",
        importpath = "github.com/jcmturner/rpc/v2",
        sum = "h1:7FXXj8Ti1IaVFpSAziCZWNzbNuZmnvw/i6CqLNdWfZY=",
        version = "v2.0.3",
    )
    go_repository(
        name = "com_github_jinzhu_gorm",
        importpath = "github.com/jinzhu/gorm",
        sum = "h1:+IyIjPEABKRpsu/F8OvDPy9fyQlgsg2luMV2ZIH5i5o=",
        version = "v1.9.16",
    )
    go_repository(
        name = "com_github_jinzhu_inflection",
        importpath = "github.com/jinzhu/inflection",
        sum = "h1:K317FqzuhWc8YvSVlFMCCUb36O/S9MCKRDI7QkRKD/E=",
        version = "v1.0.0",
    )
    go_repository(
        name = "com_github_jinzhu_now",
        importpath = "github.com/jinzhu/now",
        sum = "h1:/o9tlHleP7gOFmsnYNz3RGnqzefHA47wQpKrrdTIwXQ=",
        version = "v1.1.5",
    )
    go_repository(
        name = "com_github_jmespath_go_jmespath",
        importpath = "github.com/jmespath/go-jmespath",
        sum = "h1:BEgLn5cpjn8UN1mAw4NjwDrS35OdebyEtFe+9YPoQUg=",
        version = "v0.4.0",
    )
    go_repository(
        name = "com_github_jmespath_go_jmespath_internal_testify",
        importpath = "github.com/jmespath/go-jmespath/internal/testify",
        sum = "h1:shLQSRRSCCPj3f2gpwzGwWFoC7ycTf1rcQZHOlsJ6N8=",
        version = "v1.5.1",
    )
    go_repository(
        name = "com_github_jmoiron_sqlx",
        importpath = "github.com/jmoiron/sqlx",
        sum = "h1:vFFPA71p1o5gAeqtEAwLU4dnX2napprKtHr7PYIcN3g=",
        version = "v1.3.5",
    )
    go_repository(
        name = "com_github_josharian_intern",
        importpath = "github.com/josharian/intern",
        sum = "h1:vlS4z54oSdjm0bgjRigI+G1HpF+tI+9rE5LLzOg8HmY=",
        version = "v1.0.0",
    )
    go_repository(
        name = "com_github_json_iterator_go",
        importpath = "github.com/json-iterator/go",
        sum = "h1:PV8peI4a0ysnczrg+LtxykD8LfKY9ML6u2jnxaEnrnM=",
        version = "v1.1.12",
    )
    go_repository(
        name = "com_github_julienschmidt_httprouter",
        importpath = "github.com/julienschmidt/httprouter",
        sum = "h1:U0609e9tgbseu3rBINet9P48AI/D3oJs4dN7jwJOQ1U=",
        version = "v1.3.0",
    )
    go_repository(
        name = "com_github_karrick_godirwalk",
        importpath = "github.com/karrick/godirwalk",
        sum = "h1:b4kY7nqDdioR/6qnbHQyDvmA17u5G1cZ6J+CZXwSWoI=",
        version = "v1.17.0",
    )
    go_repository(
        name = "com_github_kballard_go_shellquote",
        importpath = "github.com/kballard/go-shellquote",
        sum = "h1:Z9n2FFNUXsshfwJMBgNA0RU6/i7WVaAegv3PtuIHPMs=",
        version = "v0.0.0-20180428030007-95032a82bc51",
    )
    go_repository(
        name = "com_github_keybase_go_keychain",
        importpath = "github.com/keybase/go-keychain",
        sum = "h1:IsMZxCuZqKuao2vNdfD82fjjgPLfyHLpR41Z88viRWs=",
        version = "v0.0.0-20231219164618-57a3676c3af6",
    )
    go_repository(
        name = "com_github_khan_genqlient",
        importpath = "github.com/Khan/genqlient",
        sum = "h1:GZ1meyRnzcDTK48EjqB8t3bcfYvHArCUUvgOwpz1D4w=",
        version = "v0.7.0",
    )
    go_repository(
        name = "com_github_kisielk_errcheck",
        importpath = "github.com/kisielk/errcheck",
        sum = "h1:e8esj/e4R+SAOwFwN+n3zr0nYeCyeweozKfO23MvHzY=",
        version = "v1.5.0",
    )
    go_repository(
        name = "com_github_kisielk_gotool",
        importpath = "github.com/kisielk/gotool",
        sum = "h1:AV2c/EiW3KqPNT9ZKl07ehoAGi4C5/01Cfbblndcapg=",
        version = "v1.0.0",
    )
    go_repository(
        name = "com_github_klauspost_compress",
        importpath = "github.com/klauspost/compress",
        sum = "h1:NE3C767s2ak2bweCZo3+rdP4U/HoyVXLv/X9f2gPS5g=",
        version = "v1.17.1",
    )
    go_repository(
        name = "com_github_klauspost_cpuid_v2",
        importpath = "github.com/klauspost/cpuid/v2",
        sum = "h1:0E5MSMDEoAulmXNFquVs//DdoomxaoTY1kUhbc/qbZg=",
        version = "v2.2.5",
    )
    go_repository(
        name = "com_github_knadh_koanf_maps",
        importpath = "github.com/knadh/koanf/maps",
        sum = "h1:G5TjmUh2D7G2YWf5SQQqSiHRJEjaicvU0KpypqB3NIs=",
        version = "v0.1.1",
    )
    go_repository(
        name = "com_github_knadh_koanf_providers_confmap",
        importpath = "github.com/knadh/koanf/providers/confmap",
        sum = "h1:gOkxhHkemwG4LezxxN8DMOFopOPghxRVp7JbIvdvqzU=",
        version = "v0.1.0",
    )
    go_repository(
        name = "com_github_knadh_koanf_v2",
        importpath = "github.com/knadh/koanf/v2",
        sum = "h1:sEZzPW2rVWSahcYILNq/syJdEyRafZIG0l9aWwL86HA=",
        version = "v2.0.2",
    )
    go_repository(
        name = "com_github_kr_pretty",
        importpath = "github.com/kr/pretty",
        sum = "h1:flRD4NNwYAUpkphVc1HcthR4KEIFJ65n8Mw5qdRn3LE=",
        version = "v0.3.1",
    )
    go_repository(
        name = "com_github_kr_text",
        importpath = "github.com/kr/text",
        sum = "h1:5Nx0Ya0ZqY2ygV366QzturHI13Jq95ApcVaJBhpS+AY=",
        version = "v0.2.0",
    )
    go_repository(
        name = "com_github_kylelemons_godebug",
        importpath = "github.com/kylelemons/godebug",
        sum = "h1:RPNrshWIDI6G2gRW9EHilWtl7Z6Sb1BR0xunSBf0SNc=",
        version = "v1.1.0",
    )
    go_repository(
        name = "com_github_labstack_echo",
        importpath = "github.com/labstack/echo",
        sum = "h1:pGRcYk231ExFAyoAjAfD85kQzRJCRI8bbnE7CX5OEgg=",
        version = "v3.3.10+incompatible",
    )
    go_repository(
        name = "com_github_labstack_echo_v4",
        importpath = "github.com/labstack/echo/v4",
        sum = "h1:dEpLU2FLg4UVmvCGPuk/APjlH6GDpbEPti61srUUUs4=",
        version = "v4.11.1",
    )
    go_repository(
        name = "com_github_labstack_gommon",
        importpath = "github.com/labstack/gommon",
        sum = "h1:y7cvthEAEbU0yHOf4axH8ZG2NH8knB9iNSoTO8dyIk8=",
        version = "v0.4.0",
    )
    go_repository(
        name = "com_github_leodido_go_urn",
        importpath = "github.com/leodido/go-urn",
        sum = "h1:XlAE/cm/ms7TE/VMVoduSpNBoyc2dOxHs5MZSwAN63Q=",
        version = "v1.2.4",
    )
    go_repository(
        name = "com_github_lestrrat_go_blackmagic",
        importpath = "github.com/lestrrat-go/blackmagic",
        sum = "h1:Cg2gVSc9h7sz9NOByczrbUvLopQmXrfFx//N+AkAr5k=",
        version = "v1.0.2",
    )
    go_repository(
        name = "com_github_lestrrat_go_httpcc",
        importpath = "github.com/lestrrat-go/httpcc",
        sum = "h1:ydWCStUeJLkpYyjLDHihupbn2tYmZ7m22BGkcvZZrIE=",
        version = "v1.0.1",
    )
    go_repository(
        name = "com_github_lestrrat_go_httprc",
        importpath = "github.com/lestrrat-go/httprc",
        sum = "h1:qgmgIRhpvBqexMJjA/PmwSvhNk679oqD1RbovdCGW8k=",
        version = "v1.0.6",
    )
    go_repository(
        name = "com_github_lestrrat_go_iter",
        importpath = "github.com/lestrrat-go/iter",
        sum = "h1:gMXo1q4c2pHmC3dn8LzRhJfP1ceCbgSiT9lUydIzltI=",
        version = "v1.0.2",
    )
    go_repository(
        name = "com_github_lestrrat_go_jwx_v2",
        importpath = "github.com/lestrrat-go/jwx/v2",
        sum = "h1:Ud4lb2QuxRClYAmRleF50KrbKIoM1TddXgBrneT5/Jo=",
        version = "v2.1.3",
    )
    go_repository(
        name = "com_github_lestrrat_go_option",
        importpath = "github.com/lestrrat-go/option",
        sum = "h1:oAzP2fvZGQKWkvHa1/SAcFolBEca1oN+mQ7eooNBEYU=",
        version = "v1.0.1",
    )
    go_repository(
        name = "com_github_lib_pq",
        importpath = "github.com/lib/pq",
        sum = "h1:AqzbZs4ZoCBp+GtejcpCpcxM3zlSMx29dXbUSeVtJb8=",
        version = "v1.10.2",
    )
    go_repository(
        name = "com_github_lufia_plan9stats",
        importpath = "github.com/lufia/plan9stats",
        sum = "h1:VtwQ41oftZwlMnOEbMWQtSEUgU64U4s+GHk7hZK+jtY=",
        version = "v0.0.0-20220913051719-115f729f3c8c",
    )
    go_repository(
        name = "com_github_mailru_easyjson",
        importpath = "github.com/mailru/easyjson",
        sum = "h1:UGYAvKxe3sBsEDzO8ZeWOSlIQfWFlxbzLZe7hwFURr0=",
        version = "v0.7.7",
    )
    go_repository(
        name = "com_github_mattn_go_colorable",
        importpath = "github.com/mattn/go-colorable",
        sum = "h1:fFA4WZxdEF4tXPZVKMLwD8oUnCTTo08duU7wxecdEvA=",
        version = "v0.1.13",
    )
    go_repository(
        name = "com_github_mattn_go_isatty",
        importpath = "github.com/mattn/go-isatty",
        sum = "h1:xfD0iDuEKnDkl03q4limB+vH+GxLEtL/jb4xVJSWWEY=",
        version = "v0.0.20",
    )
    go_repository(
        name = "com_github_mattn_go_runewidth",
        importpath = "github.com/mattn/go-runewidth",
        sum = "h1:UNAjwbU9l54TA3KzvqLGxwWjHmMgBUVhBiTjelZgg3U=",
        version = "v0.0.15",
    )
    go_repository(
        name = "com_github_mattn_go_sqlite3",
        importpath = "github.com/mattn/go-sqlite3",
        sum = "h1:JL0eqdCOq6DJVNPSvArO/bIV9/P7fbGrV00LZHc+5aI=",
        version = "v1.14.18",
    )
    go_repository(
        name = "com_github_mattn_go_zglob",
        importpath = "github.com/mattn/go-zglob",
        sum = "h1:mP8RnmCgho4oaUYDIDn6GNxYk+qJGUs8fJLn+twYj2A=",
        version = "v0.0.6",
    )
    go_repository(
        name = "com_github_microsoft_go_mssqldb",
        importpath = "github.com/microsoft/go-mssqldb",
        sum = "h1:p2rpHIL7TlSv1QrbXJUAcbyRKnIT0C9rRkH2E4OjLn8=",
        version = "v0.21.0",
    )
    go_repository(
        name = "com_github_microsoft_go_winio",
        importpath = "github.com/Microsoft/go-winio",
        sum = "h1:9/kr64B9VUZrLm5YYwbGtUJnMgqWVOdUAXu6Migciow=",
        version = "v0.6.1",
    )
    go_repository(
        name = "com_github_miekg_dns",
        importpath = "github.com/miekg/dns",
        sum = "h1:GoQ4hpsj0nFLYe+bWiCToyrBEJXkQfOOIvFGFy0lEgo=",
        version = "v1.1.55",
    )
    go_repository(
        name = "com_github_mitchellh_cli",
        importpath = "github.com/mitchellh/cli",
        sum = "h1:iGBIsUe3+HZ/AD/Vd7DErOt5sU9fa8Uj7A2s1aggv1Y=",
        version = "v1.0.0",
    )
    go_repository(
        name = "com_github_mitchellh_copystructure",
        importpath = "github.com/mitchellh/copystructure",
        sum = "h1:vpKXTN4ewci03Vljg/q9QvCGUDttBOGBIa15WveJJGw=",
        version = "v1.2.0",
    )
    go_repository(
        name = "com_github_mitchellh_go_homedir",
        importpath = "github.com/mitchellh/go-homedir",
        sum = "h1:lukF9ziXFxDFPkA1vsr5zpc1XuPDn/wFntq5mG+4E0Y=",
        version = "v1.1.0",
    )
    go_repository(
        name = "com_github_mitchellh_go_wordwrap",
        importpath = "github.com/mitchellh/go-wordwrap",
        sum = "h1:6GlHJ/LTGMrIJbwgdqdl2eEH8o+Exx/0m8ir9Gns0u4=",
        version = "v1.0.0",
    )
    go_repository(
        name = "com_github_mitchellh_mapstructure",
        importpath = "github.com/mitchellh/mapstructure",
        sum = "h1:cqn374mizHuIWj+OSJCajGr/phAmuMug9qIX3l9CflE=",
        version = "v1.5.1-0.20231216201459-8508981c8b6c",
    )
    go_repository(
        name = "com_github_mitchellh_reflectwalk",
        importpath = "github.com/mitchellh/reflectwalk",
        sum = "h1:G2LzWKi524PWgd3mLHV8Y5k7s6XUvT0Gef6zxSIeXaQ=",
        version = "v1.0.2",
    )
    go_repository(
        name = "com_github_modern_go_concurrent",
        importpath = "github.com/modern-go/concurrent",
        sum = "h1:TRLaZ9cD/w8PVh93nsPXa1VrQ6jlwL5oN8l14QlcNfg=",
        version = "v0.0.0-20180306012644-bacd9c7ef1dd",
    )
    go_repository(
        name = "com_github_modern_go_reflect2",
        importpath = "github.com/modern-go/reflect2",
        sum = "h1:xBagoLtFs94CBntxluKeaWgTMpvLxC4ur3nMaC9Gz0M=",
        version = "v1.0.2",
    )
    go_repository(
        name = "com_github_montanaflynn_stats",
        importpath = "github.com/montanaflynn/stats",
        sum = "h1:r3y12KyNxj/Sb/iOE46ws+3mS1+MZca1wlHQFPsY/JU=",
        version = "v0.7.0",
    )
    go_repository(
        name = "com_github_munnerz_goautoneg",
        importpath = "github.com/munnerz/goautoneg",
        sum = "h1:C3w9PqII01/Oq1c1nUAm88MOHcQC9l5mIlSMApZMrHA=",
        version = "v0.0.0-20191010083416-a7dc8b61c822",
    )
    go_repository(
        name = "com_github_oleiade_reflections",
        importpath = "github.com/oleiade/reflections",
        sum = "h1:D+I/UsXQB4esMathlt0kkZRJZdUDmhv5zGi/HOwYTWo=",
        version = "v1.1.0",
    )
    go_repository(
        name = "com_github_open_telemetry_opentelemetry_collector_contrib_pkg_sampling",
        importpath = "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/sampling",
        sum = "h1:iNr5/wS/0Rg4PnPO2Zf3Yj4Qc1RooVQ/7U7jKzocyPo=",
        version = "v0.104.0",
    )
    go_repository(
        name = "com_github_open_telemetry_opentelemetry_collector_contrib_processor_probabilisticsamplerprocessor",
        importpath = "github.com/open-telemetry/opentelemetry-collector-contrib/processor/probabilisticsamplerprocessor",
        sum = "h1:W2OartqDicbzoLjAp2MCi+FIt2FBy5PyeYce0kIuerc=",
        version = "v0.104.0",
    )
    go_repository(
        name = "com_github_opencontainers_runtime_spec",
        importpath = "github.com/opencontainers/runtime-spec",
        sum = "h1:l04uafi6kxByhbxev7OWiuUv0LZxEsYUfDWZ6bztAuU=",
        version = "v1.1.0-rc.3",
    )
    go_repository(
        name = "com_github_opentracing_opentracing_go",
        importpath = "github.com/opentracing/opentracing-go",
        sum = "h1:uEJPy/1a5RIPAJ0Ov+OIO8OxWu77jEv+1B0VhjKrZUs=",
        version = "v1.2.0",
    )
    go_repository(
        name = "com_github_outcaste_io_ristretto",
        importpath = "github.com/outcaste-io/ristretto",
        sum = "h1:AK4zt/fJ76kjlYObOeNwh4T3asEuaCmp26pOvUOL9w0=",
        version = "v0.2.3",
    )
    go_repository(
        name = "com_github_pborman_uuid",
        importpath = "github.com/pborman/uuid",
        sum = "h1:+ZZIw58t/ozdjRaXh/3awHfmWRbzYxJoAdNJxe/3pvw=",
        version = "v1.2.1",
    )
    go_repository(
        name = "com_github_pelletier_go_toml_v2",
        importpath = "github.com/pelletier/go-toml/v2",
        sum = "h1:uH2qQXheeefCCkuBBSLi7jCiSmj3VRh2+Goq2N7Xxu0=",
        version = "v2.0.9",
    )
    go_repository(
        name = "com_github_philhofer_fwd",
        importpath = "github.com/philhofer/fwd",
        sum = "h1:jYi87L8j62qkXzaYHAQAhEapgukhenIMZRBKTNRLHJ4=",
        version = "v1.1.3-0.20240612014219-fbbf4953d986",
    )
    go_repository(
        name = "com_github_pierrec_lz4_v4",
        importpath = "github.com/pierrec/lz4/v4",
        sum = "h1:xaKrnTkyoqfh1YItXl56+6KJNVYWlEEPuAQW9xsplYQ=",
        version = "v4.1.18",
    )
    go_repository(
        name = "com_github_pkg_browser",
        importpath = "github.com/pkg/browser",
        sum = "h1:+mdjkGKdHQG3305AYmdv1U2eRNDiU2ErMBj1gwrq8eQ=",
        version = "v0.0.0-20240102092130-5ac0b6a4141c",
    )
    go_repository(
        name = "com_github_pkg_errors",
        importpath = "github.com/pkg/errors",
        sum = "h1:FEBLx1zS214owpjy7qsBeixbURkuhQAwrK5UwLGTwt4=",
        version = "v0.9.1",
    )
    go_repository(
        name = "com_github_planetscale_vtprotobuf",
        importpath = "github.com/planetscale/vtprotobuf",
        sum = "h1:GFCKgmp0tecUJ0sJuv4pzYCqS9+RGSn52M3FUwPs+uo=",
        version = "v0.6.1-0.20240319094008-0393e58bdf10",
    )
    go_repository(
        name = "com_github_pmezard_go_difflib",
        importpath = "github.com/pmezard/go-difflib",
        sum = "h1:Jamvg5psRIccs7FGNTlIRMkT8wgtp5eCXdBlqhYGL6U=",
        version = "v1.0.1-0.20181226105442-5d4384ee4fb2",
    )
    go_repository(
        name = "com_github_posener_complete",
        importpath = "github.com/posener/complete",
        sum = "h1:ccV59UEOTzVDnDUEFdT95ZzHVZ+5+158q8+SJb2QV5w=",
        version = "v1.1.1",
    )
    go_repository(
        name = "com_github_power_devops_perfstat",
        importpath = "github.com/power-devops/perfstat",
        sum = "h1:NRoLoZvkBTKvR5gQLgA3e0hqjkY9u1wm+iOL45VN/qI=",
        version = "v0.0.0-20220216144756-c35f1ee13d7c",
    )
    go_repository(
        name = "com_github_prometheus_client_golang",
        importpath = "github.com/prometheus/client_golang",
        sum = "h1:wZWJDwK+NameRJuPGDhlnFgx8e8HN3XHQeLaYJFJBOE=",
        version = "v1.19.1",
    )
    go_repository(
        name = "com_github_prometheus_client_model",
        importpath = "github.com/prometheus/client_model",
        sum = "h1:ZKSh/rekM+n3CeS952MLRAdFwIKqeY8b62p8ais2e9E=",
        version = "v0.6.1",
    )
    go_repository(
        name = "com_github_prometheus_common",
        importpath = "github.com/prometheus/common",
        sum = "h1:ZlZy0BgJhTwVZUn7dLOkwCZHUkrAqd3WYtcFCWnM1D8=",
        version = "v0.54.0",
    )
    go_repository(
        name = "com_github_prometheus_procfs",
        importpath = "github.com/prometheus/procfs",
        sum = "h1:A82kmvXJq2jTu5YUhSGNlYoxh85zLnKgPz4bMZgI5Ek=",
        version = "v0.15.0",
    )
    go_repository(
        name = "com_github_puerkitobio_purell",
        importpath = "github.com/PuerkitoBio/purell",
        sum = "h1:WEQqlqaGbrPkxLJWfBwQmfEAE1Z7ONdDLqrN38tNFfI=",
        version = "v1.1.1",
    )
    go_repository(
        name = "com_github_puerkitobio_urlesc",
        importpath = "github.com/PuerkitoBio/urlesc",
        sum = "h1:d+Bc7a5rLufV/sSk/8dngufqelfh6jnri85riMAaF/M=",
        version = "v0.0.0-20170810143723-de5bf2ad4578",
    )
    go_repository(
        name = "com_github_puzpuzpuz_xsync_v2",
        importpath = "github.com/puzpuzpuz/xsync/v2",
        sum = "h1:mVGYAvzDSu52+zaGyNjC+24Xw2bQi3kTr4QJ6N9pIIU=",
        version = "v2.5.1",
    )
    go_repository(
        name = "com_github_qri_io_jsonpointer",
        importpath = "github.com/qri-io/jsonpointer",
        sum = "h1:prVZBZLL6TW5vsSB9fFHFAMBLI4b0ri5vribQlTJiBA=",
        version = "v0.1.1",
    )
    go_repository(
        name = "com_github_qri_io_jsonschema",
        importpath = "github.com/qri-io/jsonschema",
        sum = "h1:NNFoKms+kut6ABPf6xiKNM5214jzxAhDBrPHCJ97Wg0=",
        version = "v0.2.1",
    )
    go_repository(
        name = "com_github_rcrowley_go_metrics",
        importpath = "github.com/rcrowley/go-metrics",
        sum = "h1:N/ElC8H3+5XpJzTSTfLsJV/mx9Q9g7kxmchpfZyxgzM=",
        version = "v0.0.0-20201227073835-cf1acfcdf475",
    )
    go_repository(
        name = "com_github_redis_go_redis_v9",
        importpath = "github.com/redis/go-redis/v9",
        sum = "h1:HHDteefn6ZkTtY5fGUE8tj8uy85AHk6zP7CpzIAM0y4=",
        version = "v9.6.1",
    )
    go_repository(
        name = "com_github_remyoudompheng_bigfft",
        importpath = "github.com/remyoudompheng/bigfft",
        sum = "h1:W09IVJc94icq4NjY3clb7Lk8O1qJ8BdBEF8z0ibU0rE=",
        version = "v0.0.0-20230129092748-24d4a6f8daec",
    )
    go_repository(
        name = "com_github_richardartoul_molecule",
        importpath = "github.com/richardartoul/molecule",
        sum = "h1:4+LEVOB87y175cLJC/mbsgKmoDOjrBldtXvioEy96WY=",
        version = "v1.0.1-0.20240531184615-7ca0df43c0b3",
    )
    go_repository(
        name = "com_github_rivo_uniseg",
        importpath = "github.com/rivo/uniseg",
        sum = "h1:8TfxU8dW6PdqD27gjM8MVNuicgxIjxpm4K7x4jp8sis=",
        version = "v0.4.4",
    )
    go_repository(
        name = "com_github_rogpeppe_fastuuid",
        importpath = "github.com/rogpeppe/fastuuid",
        sum = "h1:Ppwyp6VYCF1nvBTXL3trRso7mXMlRrw9ooo375wvi2s=",
        version = "v1.2.0",
    )
    go_repository(
        name = "com_github_rogpeppe_go_internal",
        importpath = "github.com/rogpeppe/go-internal",
        sum = "h1:KvO1DLK/DRN07sQ1LQKScxyZJuNnedQ5/wKSR38lUII=",
        version = "v1.13.1",
    )
    go_repository(
        name = "com_github_russross_blackfriday_v2",
        importpath = "github.com/russross/blackfriday/v2",
        sum = "h1:JIOH55/0cWyOuilr9/qlrm0BSXldqnqwMsf35Ld67mk=",
        version = "v2.1.0",
    )
    go_repository(
        name = "com_github_ryanuber_columnize",
        importpath = "github.com/ryanuber/columnize",
        sum = "h1:j1Wcmh8OrK4Q7GXY+V7SVSY8nUWQxHW5TkBe7YUl+2s=",
        version = "v2.1.0+incompatible",
    )
    go_repository(
        name = "com_github_ryanuber_go_glob",
        importpath = "github.com/ryanuber/go-glob",
        sum = "h1:iQh3xXAumdQ+4Ufa5b25cRpC5TYKlno6hsv6Cb3pkBk=",
        version = "v1.0.0",
    )
    go_repository(
        name = "com_github_secure_systems_lab_go_securesystemslib",
        importpath = "github.com/secure-systems-lab/go-securesystemslib",
        sum = "h1:OwvJ5jQf9LnIAS83waAjPbcMsODrTQUpJ02eNLUoxBg=",
        version = "v0.7.0",
    )
    go_repository(
        name = "com_github_segmentio_asm",
        importpath = "github.com/segmentio/asm",
        sum = "h1:9BQrFxC+YOHJlTlHGkTrFWf59nbL3XnCoFLTwDCI7ys=",
        version = "v1.2.0",
    )
    go_repository(
        name = "com_github_segmentio_kafka_go",
        importpath = "github.com/segmentio/kafka-go",
        sum = "h1:qffhBZCz4WcWyNuHEclHjIMLs2slp6mZO8px+5W5tfU=",
        version = "v0.4.42",
    )
    go_repository(
        name = "com_github_sergi_go_diff",
        importpath = "github.com/sergi/go-diff",
        sum = "h1:xkr+Oxo4BOQKmkn/B9eMK0g5Kg/983T9DqqPHwYqD+8=",
        version = "v1.3.1",
    )
    go_repository(
        name = "com_github_shirou_gopsutil_v3",
        importpath = "github.com/shirou/gopsutil/v3",
        sum = "h1:dEHgzZXt4LMNm+oYELpzl9YCqV65Yr/6SfrvgRBtXeU=",
        version = "v3.24.4",
    )
    go_repository(
        name = "com_github_shoenig_go_m1cpu",
        importpath = "github.com/shoenig/go-m1cpu",
        sum = "h1:nxdKQNcEB6vzgA2E2bvzKIYRuNj7XNJ4S/aRSwKzFtM=",
        version = "v0.1.6",
    )
    go_repository(
        name = "com_github_shoenig_test",
        importpath = "github.com/shoenig/test",
        sum = "h1:kVTaSd7WLz5WZ2IaoM0RSzRsUD+m8wRR+5qvntpn4LU=",
        version = "v0.6.4",
    )
    go_repository(
        name = "com_github_shopify_sarama",
        importpath = "github.com/Shopify/sarama",
        sum = "h1:lqqPUPQZ7zPqYlWpTh+LQ9bhYNu2xJL6k1SJN4WVe2A=",
        version = "v1.38.1",
    )
    go_repository(
        name = "com_github_sirupsen_logrus",
        importpath = "github.com/sirupsen/logrus",
        sum = "h1:dueUQJ1C2q9oE3F7wvmSGAaVtTmUizReu6fjN8uqzbQ=",
        version = "v1.9.3",
    )
    go_repository(
        name = "com_github_sosodev_duration",
        importpath = "github.com/sosodev/duration",
        sum = "h1:pqK/FLSjsAADWY74SyWDCjOcd5l7H8GSnnOGEB9A1Us=",
        version = "v1.2.0",
    )
    go_repository(
        name = "com_github_spaolacci_murmur3",
        importpath = "github.com/spaolacci/murmur3",
        sum = "h1:7c1g84S4BPRrfL5Xrdp6fOJ206sU9y293DDHaoy0bLI=",
        version = "v1.1.0",
    )
    go_repository(
        name = "com_github_spf13_pflag",
        importpath = "github.com/spf13/pflag",
        sum = "h1:iy+VFUOCP1a+8yFto/drg2CJ5u0yRoB7fZw3DKv/JXA=",
        version = "v1.0.5",
    )
    go_repository(
        name = "com_github_stretchr_objx",
        importpath = "github.com/stretchr/objx",
        sum = "h1:xuMeJ0Sdp5ZMRXx/aWO6RZxdr3beISkG5/G/aIRr3pY=",
        version = "v0.5.2",
    )
    go_repository(
        name = "com_github_stretchr_testify",
        importpath = "github.com/stretchr/testify",
        sum = "h1:Xv5erBjTwe/5IxqUQTdXv5kgmIvbHo3QQyRwhJsOfJA=",
        version = "v1.10.0",
    )
    go_repository(
        name = "com_github_syndtr_goleveldb",
        importpath = "github.com/syndtr/goleveldb",
        sum = "h1:vfofYNRScrDdvS342BElfbETmL1Aiz3i2t0zfRj16Hs=",
        version = "v1.0.1-0.20220721030215-126854af5e6d",
    )
    go_repository(
        name = "com_github_tidwall_btree",
        importpath = "github.com/tidwall/btree",
        sum = "h1:LDZfKfQIBHGHWSwckhXI0RPSXzlo+KYdjK7FWSqOzzg=",
        version = "v1.6.0",
    )
    go_repository(
        name = "com_github_tidwall_buntdb",
        importpath = "github.com/tidwall/buntdb",
        sum = "h1:gdhWO+/YwoB2qZMeAU9JcWWsHSYU3OvcieYgFRS0zwA=",
        version = "v1.3.0",
    )
    go_repository(
        name = "com_github_tidwall_gjson",
        importpath = "github.com/tidwall/gjson",
        sum = "h1:SyXa+dsSPpUlcwEDuKuEBJEz5vzTvOea+9rjyYodQFg=",
        version = "v1.16.0",
    )
    go_repository(
        name = "com_github_tidwall_grect",
        importpath = "github.com/tidwall/grect",
        sum = "h1:dA3oIgNgWdSspFzn1kS4S/RDpZFLrIxAZOdJKjYapOg=",
        version = "v0.1.4",
    )
    go_repository(
        name = "com_github_tidwall_match",
        importpath = "github.com/tidwall/match",
        sum = "h1:+Ho715JplO36QYgwN9PGYNhgZvoUSc9X2c80KVTi+GA=",
        version = "v1.1.1",
    )
    go_repository(
        name = "com_github_tidwall_pretty",
        importpath = "github.com/tidwall/pretty",
        sum = "h1:qjsOFOWWQl+N3RsoF5/ssm1pHmJJwhjlSbZ51I6wMl4=",
        version = "v1.2.1",
    )
    go_repository(
        name = "com_github_tidwall_rtred",
        importpath = "github.com/tidwall/rtred",
        sum = "h1:exmoQtOLvDoO8ud++6LwVsAMTu0KPzLTUrMln8u1yu8=",
        version = "v0.1.2",
    )
    go_repository(
        name = "com_github_tidwall_tinyqueue",
        importpath = "github.com/tidwall/tinyqueue",
        sum = "h1:SpNEvEggbpyN5DIReaJ2/1ndroY8iyEGxPYxoSaymYE=",
        version = "v0.1.1",
    )
    go_repository(
        name = "com_github_tinylib_msgp",
        importpath = "github.com/tinylib/msgp",
        sum = "h1:6ypy2qcCznxpP4hpORzhtXyTqrBs7cfM9MCCWY8zsmU=",
        version = "v1.2.1",
    )
    go_repository(
        name = "com_github_tklauser_go_sysconf",
        importpath = "github.com/tklauser/go-sysconf",
        sum = "h1:0QaGUFOdQaIVdPgfITYzaTegZvdCjmYO52cSFAEVmqU=",
        version = "v0.3.12",
    )
    go_repository(
        name = "com_github_tklauser_numcpus",
        importpath = "github.com/tklauser/numcpus",
        sum = "h1:ng9scYS7az0Bk4OZLvrNXNSAO2Pxr1XXRAPyjhIx+Fk=",
        version = "v0.6.1",
    )
    go_repository(
        name = "com_github_tmthrgd_go_hex",
        importpath = "github.com/tmthrgd/go-hex",
        sum = "h1:9lRDQMhESg+zvGYmW5DyG0UqvY96Bu5QYsTLvCHdrgo=",
        version = "v0.0.0-20190904060850-447a3041c3bc",
    )
    go_repository(
        name = "com_github_twitchtv_twirp",
        importpath = "github.com/twitchtv/twirp",
        sum = "h1:+F4TdErPgSUbMZMwp13Q/KgDVuI7HJXP61mNV3/7iuU=",
        version = "v8.1.3+incompatible",
    )
    go_repository(
        name = "com_github_twitchyliquid64_golang_asm",
        importpath = "github.com/twitchyliquid64/golang-asm",
        sum = "h1:SU5vSMR7hnwNxj24w34ZyCi/FmDZTkS4MhqMhdFk5YI=",
        version = "v0.15.1",
    )
    go_repository(
        name = "com_github_ugorji_go_codec",
        importpath = "github.com/ugorji/go/codec",
        sum = "h1:BMaWp1Bb6fHwEtbplGBGJ498wD+LKlNSl25MjdZY4dU=",
        version = "v1.2.11",
    )
    go_repository(
        name = "com_github_uptrace_bun",
        importpath = "github.com/uptrace/bun",
        sum = "h1:qxBaEIo0hC/8O3O6GrMDKxqyT+mw5/s0Pn/n6xjyGIk=",
        version = "v1.1.17",
    )
    go_repository(
        name = "com_github_uptrace_bun_dialect_sqlitedialect",
        importpath = "github.com/uptrace/bun/dialect/sqlitedialect",
        sum = "h1:i8NFU9r8YuavNFaYlNqi4ppn+MgoHtqLgpWQDrVTjm0=",
        version = "v1.1.17",
    )
    go_repository(
        name = "com_github_urfave_cli",
        importpath = "github.com/urfave/cli",
        sum = "h1:MH0k6uJxdwdeWQTwhSO42Pwr4YLrNLwBtg1MRgTqPdQ=",
        version = "v1.22.16",
    )
    go_repository(
        name = "com_github_urfave_negroni",
        importpath = "github.com/urfave/negroni",
        sum = "h1:kIimOitoypq34K7TG7DUaJ9kq/N4Ofuwi1sjz0KipXc=",
        version = "v1.0.0",
    )
    go_repository(
        name = "com_github_valyala_bytebufferpool",
        importpath = "github.com/valyala/bytebufferpool",
        sum = "h1:GqA5TC/0021Y/b9FG4Oi9Mr3q7XYx6KllzawFIhcdPw=",
        version = "v1.0.0",
    )
    go_repository(
        name = "com_github_valyala_fasthttp",
        importpath = "github.com/valyala/fasthttp",
        sum = "h1:8b30A5JlZ6C7AS81RsWjYMQmrZG6feChmgAolCl1SqA=",
        version = "v1.51.0",
    )
    go_repository(
        name = "com_github_valyala_fasttemplate",
        importpath = "github.com/valyala/fasttemplate",
        sum = "h1:lxLXG0uE3Qnshl9QyaK6XJxMXlQZELvChBOCmQD0Loo=",
        version = "v1.2.2",
    )
    go_repository(
        name = "com_github_valyala_tcplisten",
        importpath = "github.com/valyala/tcplisten",
        sum = "h1:rBHj/Xf+E1tRGZyWIWwJDiRY0zc1Js+CV5DqwacVSA8=",
        version = "v1.0.0",
    )
    go_repository(
        name = "com_github_vektah_gqlparser_v2",
        importpath = "github.com/vektah/gqlparser/v2",
        sum = "h1:1gcmLTvs3JLKXckwCwlUagVn/IlV2bwqle0vJ0vy5p8=",
        version = "v2.5.16",
    )
    go_repository(
        name = "com_github_vmihailenco_bufpool",
        importpath = "github.com/vmihailenco/bufpool",
        sum = "h1:gOq2WmBrq0i2yW5QJ16ykccQ4wH9UyEsgLm6czKAd94=",
        version = "v0.1.11",
    )
    go_repository(
        name = "com_github_vmihailenco_msgpack_v4",
        importpath = "github.com/vmihailenco/msgpack/v4",
        sum = "h1:07s4sz9IReOgdikxLTKNbBdqDMLsjPKXwvCazn8G65U=",
        version = "v4.3.12",
    )
    go_repository(
        name = "com_github_vmihailenco_msgpack_v5",
        importpath = "github.com/vmihailenco/msgpack/v5",
        sum = "h1:cQriyiUvjTwOHg8QZaPihLWeRAAVoCpE00IUPn0Bjt8=",
        version = "v5.4.1",
    )
    go_repository(
        name = "com_github_vmihailenco_tagparser",
        importpath = "github.com/vmihailenco/tagparser",
        sum = "h1:gnjoVuB/kljJ5wICEEOpx98oXMWPLj22G67Vbd1qPqc=",
        version = "v0.1.2",
    )
    go_repository(
        name = "com_github_vmihailenco_tagparser_v2",
        importpath = "github.com/vmihailenco/tagparser/v2",
        sum = "h1:y09buUbR+b5aycVFQs/g70pqKVZNBmxwAhO7/IwNM9g=",
        version = "v2.0.0",
    )
    go_repository(
        name = "com_github_xdg_go_pbkdf2",
        importpath = "github.com/xdg-go/pbkdf2",
        sum = "h1:Su7DPu48wXMwC3bs7MCNG+z4FhcyEuz5dlvchbq0B0c=",
        version = "v1.0.0",
    )
    go_repository(
        name = "com_github_xdg_go_scram",
        importpath = "github.com/xdg-go/scram",
        sum = "h1:FHX5I5B4i4hKRVRBCFRxq1iQRej7WO3hhBuJf+UUySY=",
        version = "v1.1.2",
    )
    go_repository(
        name = "com_github_xdg_go_stringprep",
        importpath = "github.com/xdg-go/stringprep",
        sum = "h1:XLI/Ng3O1Atzq0oBs3TWm+5ZVgkq2aqdlvP9JtoZ6c8=",
        version = "v1.0.4",
    )
    go_repository(
        name = "com_github_youmark_pkcs8",
        importpath = "github.com/youmark/pkcs8",
        sum = "h1:splanxYIlg+5LfHAM6xpdFEAYOk8iySO56hMFq6uLyA=",
        version = "v0.0.0-20181117223130-1be2e3e5546d",
    )
    go_repository(
        name = "com_github_yuin_goldmark",
        importpath = "github.com/yuin/goldmark",
        sum = "h1:fVcFKWvrslecOb/tg+Cc05dkeYx540o0FuFt3nUVDoE=",
        version = "v1.4.13",
    )
    go_repository(
        name = "com_github_yusufpapurcu_wmi",
        importpath = "github.com/yusufpapurcu/wmi",
        sum = "h1:zFUKzehAFReQwLys1b/iSMl+JQGSCSjtVqQn9bBrPo0=",
        version = "v1.2.4",
    )
    go_repository(
        name = "com_github_zenazn_goji",
        importpath = "github.com/zenazn/goji",
        sum = "h1:4lbD8Mx2h7IvloP7r2C0D6ltZP6Ufip8Hn0wmSK5LR8=",
        version = "v1.0.1",
    )
    go_repository(
        name = "com_google_cloud_go",
        importpath = "cloud.google.com/go",
        sum = "h1:ZaGT6LiG7dBzi6zNOvVZwacaXlmf3lRqnC4DQzqyRQw=",
        version = "v0.112.2",
    )
    go_repository(
        name = "com_google_cloud_go_auth",
        importpath = "cloud.google.com/go/auth",
        sum = "h1:8Fu8TZy167JkW8Tj3q7dIkr2v4cndv41ouecJx0PAHs=",
        version = "v0.13.0",
    )
    go_repository(
        name = "com_google_cloud_go_auth_oauth2adapt",
        importpath = "cloud.google.com/go/auth/oauth2adapt",
        sum = "h1:V6a6XDu2lTwPZWOawrAa9HUK+DB2zfJyTuciBG5hFkU=",
        version = "v0.2.6",
    )
    go_repository(
        name = "com_google_cloud_go_compute",
        importpath = "cloud.google.com/go/compute",
        sum = "h1:ZRpHJedLtTpKgr3RV1Fx23NuaAEN1Zfx9hw1u4aJdjU=",
        version = "v1.25.1",
    )
    go_repository(
        name = "com_google_cloud_go_compute_metadata",
        importpath = "cloud.google.com/go/compute/metadata",
        sum = "h1:A6hENjEsCDtC1k8byVsgwvVcioamEHvZ4j01OwKxG9I=",
        version = "v0.6.0",
    )
    go_repository(
        name = "com_google_cloud_go_iam",
        importpath = "cloud.google.com/go/iam",
        sum = "h1:bEa06k05IO4f4uJonbB5iAgKTPpABy1ayxaIZV/GHVc=",
        version = "v1.1.6",
    )
    go_repository(
        name = "com_google_cloud_go_longrunning",
        importpath = "cloud.google.com/go/longrunning",
        sum = "h1:xAe8+0YaWoCKr9t1+aWe+OeQgN/iJK1fEgZSXmjuEaE=",
        version = "v0.5.6",
    )
    go_repository(
        name = "com_google_cloud_go_pubsub",
        importpath = "cloud.google.com/go/pubsub",
        sum = "h1:dfEPuGCHGbWUhaMCTHUFjfroILEkx55iUmKBZTP5f+Y=",
        version = "v1.36.1",
    )
    go_repository(
        name = "com_google_cloud_go_translate",
        importpath = "cloud.google.com/go/translate",
        sum = "h1:g+B29z4gtRGsiKDoTF+bNeH25bLRokAaElygX2FcZkE=",
        version = "v1.10.3",
    )
    go_repository(
        name = "com_lukechampine_uint128",
        importpath = "lukechampine.com/uint128",
        sum = "h1:cDdUVfRwDUDovz610ABgFD17nXD4/uDgVHl2sC3+sbo=",
        version = "v1.3.0",
    )
    go_repository(
        name = "dev_cel_expr",
        importpath = "cel.dev/expr",
        sum = "h1:NR0+oFYzR1CqLFhTAqg3ql59G9VfN8fKq1TCHJ6gq1g=",
        version = "v0.16.1",
    )
    go_repository(
        name = "dev_drjosh_zzglob",
        importpath = "drjosh.dev/zzglob",
        sum = "h1:gOb46aIHyHG8BlYpvZZM4dqR2dpsbKtI82IbYVAYIj4=",
        version = "v0.4.0",
    )
    go_repository(
        name = "im_mellium_sasl",
        importpath = "mellium.im/sasl",
        sum = "h1:wE0LW6g7U83vhvxjC1IY8DnXM+EU095yeo8XClvCdfo=",
        version = "v0.3.1",
    )
    go_repository(
        name = "in_gopkg_check_v1",
        importpath = "gopkg.in/check.v1",
        sum = "h1:Hei/4ADfdWqJk1ZMxUNpqntNwaWcugrBjAiHlqqRiVk=",
        version = "v1.0.0-20201130134442-10cb98267c6c",
    )
    go_repository(
        name = "in_gopkg_datadog_dd_trace_go_v1",
        importpath = "gopkg.in/DataDog/dd-trace-go.v1",
        sum = "h1:ZIRxAKlr3xr6xbMUDs3IDa6xq+ISv9zxyjaDCfwDjMY=",
        version = "v1.70.1",
    )
    go_repository(
        name = "in_gopkg_inf_v0",
        importpath = "gopkg.in/inf.v0",
        sum = "h1:73M5CoZyi3ZLMOyDlQh031Cx6N9NDJ2Vvfl76EDAgDc=",
        version = "v0.9.1",
    )
    go_repository(
        name = "in_gopkg_ini_v1",
        importpath = "gopkg.in/ini.v1",
        sum = "h1:Dgnx+6+nfE+IfzjUEISNeydPJh9AXNNsWbGP9KzCsOA=",
        version = "v1.67.0",
    )
    go_repository(
        name = "in_gopkg_jinzhu_gorm_v1",
        importpath = "gopkg.in/jinzhu/gorm.v1",
        sum = "h1:sTqyEcgrxG68jdeUXA9syQHNdeRhhfaYZ+vcL3x730I=",
        version = "v1.9.2",
    )
    go_repository(
        name = "in_gopkg_olivere_elastic_v3",
        importpath = "gopkg.in/olivere/elastic.v3",
        sum = "h1:u3B8p1VlHF3yNLVOlhIWFT3F1ICcHfM5V6FFJe6pPSo=",
        version = "v3.0.75",
    )
    go_repository(
        name = "in_gopkg_olivere_elastic_v5",
        importpath = "gopkg.in/olivere/elastic.v5",
        sum = "h1:acF/tRSg5geZpE3rqLglkS79CQMIMzOpWZE7hRXIkjs=",
        version = "v5.0.84",
    )
    go_repository(
        name = "in_gopkg_yaml_v2",
        importpath = "gopkg.in/yaml.v2",
        sum = "h1:D8xgwECY7CYvx+Y2n4sBz93Jn9JRvxdiyyo8CTfuKaY=",
        version = "v2.4.0",
    )
    go_repository(
        name = "in_gopkg_yaml_v3",
        importpath = "gopkg.in/yaml.v3",
        sum = "h1:fxVm/GzAzEWqLHuvctI91KS9hhNmmWOoWu0XTYJS7CA=",
        version = "v3.0.1",
    )
    go_repository(
        name = "io_gorm_driver_mysql",
        importpath = "gorm.io/driver/mysql",
        sum = "h1:omJoilUzyrAp0xNoio88lGJCroGdIOen9hq2A/+3ifw=",
        version = "v1.0.1",
    )
    go_repository(
        name = "io_gorm_driver_postgres",
        importpath = "gorm.io/driver/postgres",
        sum = "h1:1FPESNXqIKG5JmraaH2bfCVlMQ7paLoCreFxDtqzwdc=",
        version = "v1.4.6",
    )
    go_repository(
        name = "io_gorm_driver_sqlserver",
        importpath = "gorm.io/driver/sqlserver",
        sum = "h1:nMtEeKqv2R/vv9FoHUFWfXfP6SskAgRar0TPlZV1stk=",
        version = "v1.4.2",
    )
    go_repository(
        name = "io_gorm_gorm",
        importpath = "gorm.io/gorm",
        sum = "h1:zi4rHZj1anhZS2EuEODMhDisGy+Daq9jtPrNGgbQYD8=",
        version = "v1.25.3",
    )
    go_repository(
        name = "io_k8s_api",
        importpath = "k8s.io/api",
        sum = "h1:mqyHf7aoaYMpdvO87mqpol+Qnsmo+y09S0PMIXwiZKo=",
        version = "v0.25.5",
    )
    go_repository(
        name = "io_k8s_apimachinery",
        importpath = "k8s.io/apimachinery",
        sum = "h1:SQomYHvv+aO43qdu3QKRf9YuI0oI8w3RrOQ1qPbAUGY=",
        version = "v0.25.5",
    )
    go_repository(
        name = "io_k8s_client_go",
        importpath = "k8s.io/client-go",
        sum = "h1:7QWVK0Ph4bLn0UwotPTc2FTgm8shreQXyvXnnHDd8rE=",
        version = "v0.25.5",
    )
    go_repository(
        name = "io_k8s_klog_v2",
        importpath = "k8s.io/klog/v2",
        sum = "h1:7aaoSdahviPmR+XkS7FyxlkkXs6tHISSG03RxleQAVQ=",
        version = "v2.70.1",
    )
    go_repository(
        name = "io_k8s_kube_openapi",
        importpath = "k8s.io/kube-openapi",
        sum = "h1:MQ8BAZPZlWk3S9K4a9NCkIFQtZShWqoha7snGixVgEA=",
        version = "v0.0.0-20220803162953-67bda5d908f1",
    )
    go_repository(
        name = "io_k8s_sigs_json",
        importpath = "sigs.k8s.io/json",
        sum = "h1:iXTIw73aPyC+oRdyqqvVJuloN1p0AC/kzH07hu3NE+k=",
        version = "v0.0.0-20220713155537-f223a00ba0e2",
    )
    go_repository(
        name = "io_k8s_sigs_structured_merge_diff_v4",
        importpath = "sigs.k8s.io/structured-merge-diff/v4",
        sum = "h1:PRbqxJClWWYMNV1dhaG4NsibJbArud9kFxnAMREiWFE=",
        version = "v4.2.3",
    )
    go_repository(
        name = "io_k8s_sigs_yaml",
        importpath = "sigs.k8s.io/yaml",
        sum = "h1:kr/MCeFWJWTwyaHoR9c8EjH9OumOmoF9YGiZd7lFm/Q=",
        version = "v1.2.0",
    )
    go_repository(
        name = "io_k8s_utils",
        importpath = "k8s.io/utils",
        sum = "h1:jAne/RjBTyawwAy0utX5eqigAwz/lQhTmy+Hr/Cpue4=",
        version = "v0.0.0-20220728103510-ee6ede2d64ed",
    )
    go_repository(
        name = "io_opencensus_go",
        importpath = "go.opencensus.io",
        sum = "h1:y73uSU6J157QMP2kn2r30vwW1A2W2WFwSCGnAVxeaD0=",
        version = "v0.24.0",
    )
    go_repository(
        name = "io_opentelemetry_go_auto_sdk",
        importpath = "go.opentelemetry.io/auto/sdk",
        sum = "h1:cH53jehLUN6UFLY71z+NDOiNJqDdPRaXzTel0sJySYA=",
        version = "v1.1.0",
    )
    go_repository(
        name = "io_opentelemetry_go_collector",
        importpath = "go.opentelemetry.io/collector",
        sum = "h1:R3zjM4O3K3+ttzsjPV75P80xalxRbwYTURlK0ys7uyo=",
        version = "v0.104.0",
    )
    go_repository(
        name = "io_opentelemetry_go_collector_component",
        importpath = "go.opentelemetry.io/collector/component",
        sum = "h1:jqu/X9rnv8ha0RNZ1a9+x7OU49KwSMsPbOuIEykHuQE=",
        version = "v0.104.0",
    )
    go_repository(
        name = "io_opentelemetry_go_collector_config_configtelemetry",
        importpath = "go.opentelemetry.io/collector/config/configtelemetry",
        sum = "h1:eHv98XIhapZA8MgTiipvi+FDOXoFhCYOwyKReOt+E4E=",
        version = "v0.104.0",
    )
    go_repository(
        name = "io_opentelemetry_go_collector_confmap",
        importpath = "go.opentelemetry.io/collector/confmap",
        sum = "h1:O69bkeyR1YPAFz+jMd45aDZc1DtYnwb3Skgr2yALPqQ=",
        version = "v0.94.1",
    )
    go_repository(
        name = "io_opentelemetry_go_collector_consumer",
        importpath = "go.opentelemetry.io/collector/consumer",
        sum = "h1:Z1ZjapFp5mUcbkGEL96ljpqLIUMhRgQQpYKkDRtxy+4=",
        version = "v0.104.0",
    )
    go_repository(
        name = "io_opentelemetry_go_collector_pdata",
        importpath = "go.opentelemetry.io/collector/pdata",
        sum = "h1:rzYyV1zfTQQz1DI9hCiaKyyaczqawN75XO9mdXmR/hE=",
        version = "v1.11.0",
    )
    go_repository(
        name = "io_opentelemetry_go_collector_pdata_pprofile",
        importpath = "go.opentelemetry.io/collector/pdata/pprofile",
        sum = "h1:MYOIHvPlKEJbWLiBKFQWGD0xd2u22xGVLt4jPbdxP4Y=",
        version = "v0.104.0",
    )
    go_repository(
        name = "io_opentelemetry_go_collector_pdata_testdata",
        importpath = "go.opentelemetry.io/collector/pdata/testdata",
        sum = "h1:BKTZ7hIyAX5DMPecrXkVB2e86HwWtJyOlXn/5vSVXNw=",
        version = "v0.104.0",
    )
    go_repository(
        name = "io_opentelemetry_go_collector_processor",
        importpath = "go.opentelemetry.io/collector/processor",
        sum = "h1:KSvMDu4DWmK1/k2z2rOzMtTvAa00jnTabtPEK9WOSYI=",
        version = "v0.104.0",
    )
    go_repository(
        name = "io_opentelemetry_go_collector_semconv",
        importpath = "go.opentelemetry.io/collector/semconv",
        sum = "h1:dUvajnh+AYJLEW/XOPk0T0BlwltSdi3vrjO7nSOos3k=",
        version = "v0.104.0",
    )
    go_repository(
        name = "io_opentelemetry_go_contrib_instrumentation_google_golang_org_grpc_otelgrpc",
        importpath = "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc",
        sum = "h1:r6I7RJCN86bpD/FQwedZ0vSixDpwuWREjW9oRMsmqDc=",
        version = "v0.54.0",
    )
    go_repository(
        name = "io_opentelemetry_go_contrib_instrumentation_net_http_otelhttp",
        importpath = "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp",
        sum = "h1:TT4fX+nBOA/+LUkobKGW1ydGcn+G3vRw9+g5HwCphpk=",
        version = "v0.54.0",
    )
    go_repository(
        name = "io_opentelemetry_go_contrib_propagators_aws",
        importpath = "go.opentelemetry.io/contrib/propagators/aws",
        sum = "h1:MefPfPIut0IxEiQRK1qVv5AFADBOwizl189+m7QhpFg=",
        version = "v1.33.0",
    )
    go_repository(
        name = "io_opentelemetry_go_contrib_propagators_b3",
        importpath = "go.opentelemetry.io/contrib/propagators/b3",
        sum = "h1:ig/IsHyyoQ1F1d6FUDIIW5oYpsuTVtN16AyGOgdjAHQ=",
        version = "v1.33.0",
    )
    go_repository(
        name = "io_opentelemetry_go_contrib_propagators_jaeger",
        importpath = "go.opentelemetry.io/contrib/propagators/jaeger",
        sum = "h1:Jok/dG8kfp+yod29XKYV/blWgYPlMuRUoRHljrXMF5E=",
        version = "v1.33.0",
    )
    go_repository(
        name = "io_opentelemetry_go_contrib_propagators_ot",
        importpath = "go.opentelemetry.io/contrib/propagators/ot",
        sum = "h1:xj/pQFKo4ROsx0v129KpLgFwaYMgFTu3dAMEEih97cY=",
        version = "v1.33.0",
    )
    go_repository(
        name = "io_opentelemetry_go_otel",
        importpath = "go.opentelemetry.io/otel",
        sum = "h1:/FerN9bax5LoK51X/sI0SVYrjSE0/yUL7DpxW4K3FWw=",
        version = "v1.33.0",
    )
    go_repository(
        name = "io_opentelemetry_go_otel_exporters_otlp_otlptrace",
        importpath = "go.opentelemetry.io/otel/exporters/otlp/otlptrace",
        sum = "h1:Vh5HayB/0HHfOQA7Ctx69E/Y/DcQSMPpKANYVMQ7fBA=",
        version = "v1.33.0",
    )
    go_repository(
        name = "io_opentelemetry_go_otel_exporters_otlp_otlptrace_otlptracegrpc",
        importpath = "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc",
        sum = "h1:5pojmb1U1AogINhN3SurB+zm/nIcusopeBNp42f45QM=",
        version = "v1.33.0",
    )
    go_repository(
        name = "io_opentelemetry_go_otel_exporters_prometheus",
        importpath = "go.opentelemetry.io/otel/exporters/prometheus",
        sum = "h1:Er5I1g/YhfYv9Affk9nJLfH/+qCCVVg1f2R9AbJfqDQ=",
        version = "v0.49.0",
    )
    go_repository(
        name = "io_opentelemetry_go_otel_metric",
        importpath = "go.opentelemetry.io/otel/metric",
        sum = "h1:r+JOocAyeRVXD8lZpjdQjzMadVZp2M4WmQ+5WtEnklQ=",
        version = "v1.33.0",
    )
    go_repository(
        name = "io_opentelemetry_go_otel_sdk",
        importpath = "go.opentelemetry.io/otel/sdk",
        sum = "h1:iax7M131HuAm9QkZotNHEfstof92xM+N8sr3uHXc2IM=",
        version = "v1.33.0",
    )
    go_repository(
        name = "io_opentelemetry_go_otel_sdk_metric",
        importpath = "go.opentelemetry.io/otel/sdk/metric",
        sum = "h1:5uGNOlpXi+Hbo/DRoI31BSb1v+OGcpv2NemcCrOL8gI=",
        version = "v1.27.0",
    )
    go_repository(
        name = "io_opentelemetry_go_otel_trace",
        importpath = "go.opentelemetry.io/otel/trace",
        sum = "h1:cCJuF7LRjUFso9LPnEAHJDB2pqzp+hbO8eu1qqW2d/s=",
        version = "v1.33.0",
    )
    go_repository(
        name = "io_opentelemetry_go_proto_otlp",
        importpath = "go.opentelemetry.io/proto/otlp",
        sum = "h1:TA9WRvW6zMwP+Ssb6fLoUIuirti1gGbP28GcKG1jgeg=",
        version = "v1.4.0",
    )
    go_repository(
        name = "org_golang_google_api",
        importpath = "google.golang.org/api",
        sum = "h1:KmF6KaDyFqB417T68tMPbVmmwtIXs2VB60OJKIHB0xQ=",
        version = "v0.213.0",
    )
    go_repository(
        name = "org_golang_google_appengine",
        importpath = "google.golang.org/appengine",
        sum = "h1:IhEN5q69dyKagZPYMSdIjS2HqprW324FRQZJcGqPAsM=",
        version = "v1.6.8",
    )
    go_repository(
        name = "org_golang_google_genproto",
        importpath = "google.golang.org/genproto",
        sum = "h1:9+tzLLstTlPTRyJTh+ah5wIMsBW5c4tQwGTN3thOW9Y=",
        version = "v0.0.0-20240213162025-012b6fc9bca9",
    )
    go_repository(
        name = "org_golang_google_genproto_googleapis_api",
        importpath = "google.golang.org/genproto/googleapis/api",
        sum = "h1:CkkIfIt50+lT6NHAVoRYEyAvQGFM7xEwXUUywFvEb3Q=",
        version = "v0.0.0-20241209162323-e6fa225c2576",
    )
    go_repository(
        name = "org_golang_google_genproto_googleapis_bytestream",
        importpath = "google.golang.org/genproto/googleapis/bytestream",
        sum = "h1:H8LrtQMZ6iQnV+zpgeb0YqwdByodQltmFqIhjuwexOI=",
        version = "v0.0.0-20241209162323-e6fa225c2576",
    )
    go_repository(
        name = "org_golang_google_genproto_googleapis_rpc",
        importpath = "google.golang.org/genproto/googleapis/rpc",
        sum = "h1:8ZmaLZE4XWrtU3MyClkYqqtl6Oegr3235h7jxsDyqCY=",
        version = "v0.0.0-20241209162323-e6fa225c2576",
    )
    go_repository(
        name = "org_golang_google_grpc",
        importpath = "google.golang.org/grpc",
        sum = "h1:oI5oTa11+ng8r8XMMN7jAOmWfPZWbYpCFaMUTACxkM0=",
        version = "v1.68.1",
    )
    go_repository(
        name = "org_golang_google_protobuf",
        importpath = "google.golang.org/protobuf",
        sum = "h1:8Ar7bF+apOIoThw1EdZl0p1oWvMqTHmpA2fRTyZO8io=",
        version = "v1.35.2",
    )
    go_repository(
        name = "org_golang_x_arch",
        importpath = "golang.org/x/arch",
        sum = "h1:A8WCeEWhLwPBKNbFi5Wv5UTCBx5zzubnXDlMOFAzFMc=",
        version = "v0.4.0",
    )
    go_repository(
        name = "org_golang_x_crypto",
        importpath = "golang.org/x/crypto",
        sum = "h1:ihbySMvVjLAeSH1IbfcRTkD/iNscyz8rGzjF/E5hV6U=",
        version = "v0.31.0",
    )
    go_repository(
        name = "org_golang_x_exp",
        importpath = "golang.org/x/exp",
        sum = "h1:mchzmB1XO2pMaKFRqk/+MV3mgGG96aqaPXaMifQU47w=",
        version = "v0.0.0-20231108232855-2478ac86f678",
    )
    go_repository(
        name = "org_golang_x_mod",
        importpath = "golang.org/x/mod",
        sum = "h1:utOm6MM3R3dnawAiJgn0y+xvuYRsm1RKM/4giyfDgV0=",
        version = "v0.20.0",
    )
    go_repository(
        name = "org_golang_x_net",
        importpath = "golang.org/x/net",
        sum = "h1:74SYHlV8BIgHIFC/LrYkOGIwL19eTYXQ5wc6TBuO36I=",
        version = "v0.33.0",
    )
    go_repository(
        name = "org_golang_x_oauth2",
        importpath = "golang.org/x/oauth2",
        sum = "h1:KTBBxWqUa0ykRPLtV69rRto9TLXcqYkeswu48x/gvNE=",
        version = "v0.24.0",
    )
    go_repository(
        name = "org_golang_x_sync",
        importpath = "golang.org/x/sync",
        sum = "h1:3NQrjDixjgGwUOCaF8w2+VYHv0Ve/vGYSbdkTa98gmQ=",
        version = "v0.10.0",
    )
    go_repository(
        name = "org_golang_x_sys",
        importpath = "golang.org/x/sys",
        sum = "h1:Fksou7UEQUWlKvIdsqzJmUmCX3cZuD2+P3XyyzwMhlA=",
        version = "v0.28.0",
    )
    go_repository(
        name = "org_golang_x_telemetry",
        importpath = "golang.org/x/telemetry",
        sum = "h1:zf5N6UOrA487eEFacMePxjXAJctxKmyjKUsjA11Uzuk=",
        version = "v0.0.0-20240521205824-bda55230c457",
    )
    go_repository(
        name = "org_golang_x_term",
        importpath = "golang.org/x/term",
        sum = "h1:WP60Sv1nlK1T6SupCHbXzSaN0b9wUmsPoRS9b61A23Q=",
        version = "v0.27.0",
    )
    go_repository(
        name = "org_golang_x_text",
        importpath = "golang.org/x/text",
        sum = "h1:zyQAAkrwaneQ066sspRyJaG9VNi/YJ1NfzcGB3hZ/qo=",
        version = "v0.21.0",
    )
    go_repository(
        name = "org_golang_x_time",
        importpath = "golang.org/x/time",
        sum = "h1:9i3RxcPv3PZnitoVGMPDKZSq1xW1gK1Xy3ArNOGZfEg=",
        version = "v0.8.0",
    )
    go_repository(
        name = "org_golang_x_tools",
        importpath = "golang.org/x/tools",
        sum = "h1:J1shsA93PJUEVaUSaay7UXAyE8aimq3GW0pjlolpa24=",
        version = "v0.24.0",
    )
    go_repository(
        name = "org_golang_x_xerrors",
        importpath = "golang.org/x/xerrors",
        sum = "h1:+cNy6SZtPcJQH3LJVLOSmiC7MMxXNOb3PU/VUEz+EhU=",
        version = "v0.0.0-20231012003039-104605ab7028",
    )
    go_repository(
        name = "org_modernc_cc_v3",
        importpath = "modernc.org/cc/v3",
        sum = "h1:QoR1Sn3YWlmA1T4vLaKZfawdVtSiGx8H+cEojbC7v1Q=",
        version = "v3.41.0",
    )
    go_repository(
        name = "org_modernc_ccgo_v3",
        importpath = "modernc.org/ccgo/v3",
        sum = "h1:KbDR3ZAVU+wiLyMESPtbtE/Add4elztFyfsWoNTgxS0=",
        version = "v3.16.15",
    )
    go_repository(
        name = "org_modernc_libc",
        importpath = "modernc.org/libc",
        sum = "h1:orZH3c5wmhIQFTXF+Nt+eeauyd+ZIt2BX6ARe+kD+aw=",
        version = "v1.37.6",
    )
    go_repository(
        name = "org_modernc_mathutil",
        importpath = "modernc.org/mathutil",
        sum = "h1:fRe9+AmYlaej+64JsEEhoWuAYBkOtQiMEU7n/XgfYi4=",
        version = "v1.6.0",
    )
    go_repository(
        name = "org_modernc_memory",
        importpath = "modernc.org/memory",
        sum = "h1:Klh90S215mmH8c9gO98QxQFsY+W451E8AnzjoE2ee1E=",
        version = "v1.7.2",
    )
    go_repository(
        name = "org_modernc_opt",
        importpath = "modernc.org/opt",
        sum = "h1:3XOZf2yznlhC+ibLltsDGzABUGVx8J6pnFMS3E4dcq4=",
        version = "v0.1.3",
    )
    go_repository(
        name = "org_modernc_sqlite",
        importpath = "modernc.org/sqlite",
        sum = "h1:Zx+LyDDmXczNnEQdvPuEfcFVA2ZPyaD7UCZDjef3BHQ=",
        version = "v1.28.0",
    )
    go_repository(
        name = "org_modernc_strutil",
        importpath = "modernc.org/strutil",
        sum = "h1:agBi9dp1I+eOnxXeiZawM8F4LawKv4NzGWSaLfyeNZA=",
        version = "v1.2.0",
    )
    go_repository(
        name = "org_modernc_token",
        importpath = "modernc.org/token",
        sum = "h1:Xl7Ap9dKaEs5kLoOQeQmPWevfnk/DM5qcLcYlA8ys6Y=",
        version = "v1.1.0",
    )
    go_repository(
        name = "org_mongodb_go_mongo_driver",
        importpath = "go.mongodb.org/mongo-driver",
        sum = "h1:nLkghSU8fQNaK7oUmDhQFsnrtcoNy7Z6LVFKsEecqgE=",
        version = "v1.12.1",
    )
    go_repository(
        name = "org_uber_go_atomic",
        importpath = "go.uber.org/atomic",
        sum = "h1:ZvwS0R+56ePWxUNi+Atn9dWONBPp/AUETXlHW0DxSjE=",
        version = "v1.11.0",
    )
    go_repository(
        name = "org_uber_go_goleak",
        importpath = "go.uber.org/goleak",
        sum = "h1:2K3zAYmnTNqV73imy9J1T3WC+gmCePx2hEGkimedGto=",
        version = "v1.3.0",
    )
    go_repository(
        name = "org_uber_go_multierr",
        importpath = "go.uber.org/multierr",
        sum = "h1:blXXJkSxSSfBVBlC76pxqeO+LN3aDfLQo+309xJstO0=",
        version = "v1.11.0",
    )
    go_repository(
        name = "org_uber_go_zap",
        importpath = "go.uber.org/zap",
        sum = "h1:aJMhYGrd5QSmlpLMr2MftRKl7t8J8PTZPA732ud/XR8=",
        version = "v1.27.0",
    )
    go_repository(
        name = "tech_einride_go_aip",
        importpath = "go.einride.tech/aip",
        sum = "h1:XfV+NQX6L7EOYK11yoHHFtndeaWh3KbD9/cN/6iWEt8=",
        version = "v0.66.0",
    )
    go_repository(
        name = "tools_gotest_v3",
        importpath = "gotest.tools/v3",
        sum = "h1:EENdUnS3pdur5nybKYIh2Vfgc8IUNBjxDPSjtiJcOzU=",
        version = "v3.5.1",
    )
