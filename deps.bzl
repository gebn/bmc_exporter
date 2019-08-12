load("@bazel_gazelle//:deps.bzl", "go_repository")

def _maybe(repo_rule, name, **kwargs):
    if name not in native.existing_rules():
        repo_rule(name = name, **kwargs)

def _kingpin():
    _maybe(
        go_repository,
        name = "com_github_alecthomas_kingpin",
        importpath = "github.com/alecthomas/kingpin",
        tag = "v2.2.6",
    )

    _maybe(
        go_repository,
        name = "com_github_alecthomas_units",
        commit = "2efee857e7cfd4f3d0138cc3cbb1b4966962b93a",  # master as of 2015-10-22
        importpath = "github.com/alecthomas/units",
    )

    _maybe(
        go_repository,
        name = "com_github_alecthomas_template",
        commit = "a0175ee3bccc567396460bf5acd36800cb10c49c",  # master as of 2016-04-05
        importpath = "github.com/alecthomas/template",
    )

def deps():
    _kingpin()

    _maybe(
        go_repository,
        name = "com_github_gebn_go_stamp",
        tag = "v2.0.1",
        importpath = "github.com/gebn/go-stamp",
    )

    _maybe(
        go_repository,
        name = "com_github_go_yaml_yaml",
        tag = "v2.2.2",
        importpath = "github.com/go-yaml/yaml",
    )

    #go_repository(
    #    name = "com_github_fsnotify_fsnotify",
    #    tag = "v1.4.7",
    #    importpath = "github.com/fsnotify/fsnotify",
    #)
