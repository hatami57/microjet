package host

// DBOption tunes a single database registration. Options are applied to the
// service before it is stored, so they can override values derived from the
// registration name.
type DBOption func(*databaseService)

// Section overrides the config section a database loads. By default the section
// is derived from the registration name ([database] for the default database,
// [database.<name>] for a named one); use Section when the logical name and the
// config key differ, e.g.:
//
//	app.WithNamedDatabase("bot", gormx.SQLite(), host.Section("legacy"))
func Section(name string) DBOption {
	return func(d *databaseService) { d.section = name }
}
