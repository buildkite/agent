load("@gazelle//:deps.bzl", "go_repository")

def go_dependencies():
    go_repository(
        name = "com_google_cloud_go_compute_metadata",
        importpath = "cloud.google.com/go/compute/metadata",
        sum = "",
        version = "v0.6.0",
    )
