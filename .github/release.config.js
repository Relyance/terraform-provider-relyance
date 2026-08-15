const githubPlugin = "@semantic-release/github";

module.exports = {
    "branches": [
        {name: "main"},
    ],
    "tagFormat": "v${version}",
    "preset": "conventionalcommits",
    "presetConfig": {
    },
    "releaseRules": [
          { "type": "docs", "release": "patch" },
          { "type": "style", "release": "patch" },
          { "type": "refactor", "release": "patch" },
          { "type": "perf", "release": "patch" },
          { "type": "test", "release": "patch" },
          { "type": "build", "release": "patch" },
          { "type": "ci", "release": "patch" },
          { "type": "chore", "release": "patch" },
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
