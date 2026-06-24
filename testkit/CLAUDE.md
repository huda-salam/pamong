# testkit/ — Testing Utilities

Mock dan helper untuk testing modul tanpa infra nyata.
- MockRepo[T], MockPublisher, MockMetrics, NoopLogger
- NewTestApp(t), NewContext(t, WithRole/WithPersona)
- Seed[T](t, db, entity) untuk integration test
- NewTestDB(t) via testcontainers (Postgres)
- AssertEventSchema(t, name, payload) untuk contract test
