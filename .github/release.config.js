const githubPlugin = "@semantic-release/github";

module.exports = {
    "branches": [
        {name: "main"},
    ],
    "tagFormat": "v${version}",
    "preset": "conventionalcommits",
    "presetConfig": {
    },
    // `chore`, `build` and `docs` are deliberately ABSENT: they fall through to the
    // conventionalcommits preset, which does not release. A published provider version is
    // something customers pin, so dependency bumps, CI tweaks and README edits must not mint
    // one on their own -- they ride along with the next feat/fix instead. Everything still
    // listed here does cut a patch.
    "releaseRules": [
          { "type": "style", "release": "patch" },
          { "type": "refactor", "release": "patch" },
          { "type": "perf", "release": "patch" },
          { "type": "test", "release": "patch" },
          { "type": "ci", "release": "patch" },
          { "type": "revert", "release": "patch" },
        ],
    "parserOpts": {
        "mergePattern": "^Merge pull request #(\\d+) from (.*)$",
        "mergeCorrespondence": ["id", "source"],
        "noteKeywords": ["BREAKING CHANGE", "BREAKING CHANGES"]
    },
    "plugins": [
        "@semantic-release/commit-analyzer",
        "@semantic-release/release-notes-generator",
        githubPlugin
    ]
}
