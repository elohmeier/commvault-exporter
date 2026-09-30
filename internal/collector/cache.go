package collector

import "time"

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func (e *Exporter) subcollectorNames(module string) []string {
	switch module {
	case "dashboard":
		names := []string{"commcell_details", "sla", "jobs_24h", "health_overview"}
		if e.cfg.Paths.Environment != "" {
			names = append(names, "environment")
		}
		return names
	case "storage":
		return []string{"pools", "policies", "media_agents", "storage_space_usage", "libraries"}
	case "licensing":
		return []string{"commcell_license", "current_capacity", "operating_instances", "endpoint_users", "hyperscale_storage", "airgap_protect", "data_insights"}
	default:
		return nil
	}
}

func (e *Exporter) subcollectorStatusLocked(now time.Time) []cacheSubcollectorStatus {
	statuses := []cacheSubcollectorStatus{}
	for _, name := range moduleNames {
		if e.cfg.IsModuleDisabled(name) {
			continue
		}
		for _, sub := range e.subcollectorNames(name) {
			snapshot, ok := e.subSnapshots[name+"/"+sub]
			statuses = append(statuses, cacheSubcollectorStatus{
				Collector: name, Subcollector: sub, LastSuccessUnix: unixOrZero(snapshot.Published),
				Stale: !ok || now.Sub(snapshot.Published) > e.cfg.MaxStale,
			})
		}
	}
	return statuses
}
