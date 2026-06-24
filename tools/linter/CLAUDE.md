# tools/linter — Custom Go Analyzer

Satu package per rule, satu Analyzer, test pakai analysistest.
Semua rule didaftarkan di registry.go → dijalankan via pamongctl lint.

Rules:
- domain-no-infra-import
- handler-must-check-permission
- handler-no-direct-repo
- event-must-use-const
- permission-must-be-registered
- entity-explicit-auditable
- raw-sql-must-annotate
- config-no-direct-env
- migration-needs-down
- no-cross-module-import
- no-cross-schema-join
- workflow-action-no-logic
- tenant-branch-must-be-strategy
- strategy-key-must-be-registered
