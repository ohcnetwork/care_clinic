package main

import "github.com/ohcnetwork/care_desktop/app/internal/plugins"

func (a *App) ReadPlugins() ([]plugins.Plugin, error) {
	var list []plugins.Plugin
	err := a.withReadJob(func() error {
		if err := a.requireSetup(); err != nil {
			return err
		}
		var err error
		list, err = plugins.New(a.installDir()).ReadPlugins()
		return err
	})
	return list, err
}

func (a *App) SavePlugins(pluginList []plugins.Plugin) error {
	return a.withJob(func() error {
		if err := a.requireStableClinic(); err != nil {
			return err
		}
		return plugins.New(a.installDir()).SavePlugins(pluginList)
	})
}

func (a *App) PluginCatalog() ([]plugins.CatalogEntry, error) {
	return plugins.Catalog()
}
