def _maybe(repo_rule, name, **kwargs):
    if name not in native.existing_rules():
        repo_rule(name = name, **kwargs)

def _kingpin(go_repository):
    _maybe(
        go_repository,
        name = "com_github_alecthomas_kingpin",
        importpath = "github.com/alecthomas/kingpin",
        tag = "v2.2.6",
    )

    _maybe(
        go_repository,
        name = "com_github_alecthomas_units",
        commit = "f65c72e2690dc4b403c8bd637baf4611cd4c069b",
        importpath = "github.com/alecthomas/units",
    )

    _maybe(
        go_repository,
        name = "com_github_alecthomas_template",
        commit = "fb15b899a75114aa79cc930e33c46b577cc664b1",
        importpath = "github.com/alecthomas/template",
    )

def deps(go_repository):
    _kingpin(go_repository)

    _maybe(
        go_repository,
        name = "com_github_gebn_go_stamp",
        tag = "v2.0.2",
        importpath = "github.com/gebn/go-stamp",
    )

    _maybe(
        go_repository,
        name = "com_github_go_yaml_yaml",
        tag = "v2.3.0",
        importpath = "github.com/go-yaml/yaml",
    )
