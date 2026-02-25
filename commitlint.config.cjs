// commitlint configuration – Conventional Commits rule set.
// Used by wagoid/commitlint-action in .github/workflows/commitlint.yml.
// See https://commitlint.js.org for full documentation.

/** @type {import('@commitlint/types').UserConfig} */
module.exports = {
    extends: ['@commitlint/config-conventional'],
    rules: {
        // Allowed commit types (mirrors the PR title workflow).
        'type-enum': [
            2,
            'always',
            [
                'feat',     // A new feature
                'fix',      // A bug fix
                'docs',     // Documentation-only changes
                'style',    // Formatting, missing semi-colons, etc. – no logic change
                'refactor', // Neither a bug fix nor a new feature
                'perf',     // Performance improvement
                'test',     // Adding or updating tests
                'build',    // Build system or external dependency changes
                'ci',       // CI/CD configuration changes
                'chore',    // Other changes that don't modify src or test files
                'revert',   // Reverts a previous commit
            ],
        ],
        // Subject must not start with an uppercase letter but may contain
        // uppercase abbreviations (e.g. CI, CD, API, URL) anywhere else.
        // 'lower-case' (every character lowercase) is intentionally NOT used.
        'subject-case': [2, 'always', 'start-with-lower-case'],
        // Header (type + scope + subject) must not exceed 100 characters.
        'header-max-length': [2, 'always', 100],
    },
};
