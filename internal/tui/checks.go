package tui

import (
	"path/filepath"

	"github.com/uzielvgx/vgxness/internal/claudecli"
)

func newer(candidate, current string) bool {
	return candidate != current && claudecli.AtLeast(candidate, current)
}

func binaryCheck(data Overview) checkRow {
	return checkRow{state: stateOK, name: "vgxness", value: version(data.Binary.Version), detail: displayPath(data.Binary.Path, 34)}
}

func claudeCheck(data Overview) checkRow {
	minimum := data.Claude.Minimum
	if minimum == "" {
		minimum = claudecli.MinimumVersion
	}
	switch {
	case data.Claude.Version == "":
		return checkRow{state: stateError, name: "Claude Code", value: "no encontrado", detail: "se necesita " + version(minimum) + " o superior", action: "instala Claude Code y actualiza con [r]"}
	case !claudecli.AtLeast(data.Claude.Version, minimum):
		return checkRow{state: stateWarn, name: "Claude Code", value: version(data.Claude.Version), detail: "mínimo " + version(minimum), action: "actualiza Claude Code"}
	default:
		return checkRow{state: stateOK, name: "Claude Code", value: version(data.Claude.Version), detail: "mínimo " + version(minimum)}
	}
}

func pluginCheck(data Overview, commands bool) checkRow {
	plugin := data.Plugin
	row := checkRow{name: "plugin", value: version(plugin.Version)}
	switch {
	case plugin.Err != nil && !plugin.Installed:
		row.state, row.value, row.detail = stateWarn, "sin datos", "no se pudo consultar claude plugin list"
	case !plugin.Installed && plugin.MarketplaceAdded:
		row.state, row.value, row.detail = stateError, "no instalado", "marketplace agregado · falta instalar"
		row.command = "claude plugin install " + claudecli.PluginID
	case !plugin.Installed:
		row.state, row.value, row.detail = stateError, "no instalado", "falta marketplace "+claudecli.MarketplaceRepo
		row.command = "claude plugin marketplace add " + claudecli.MarketplaceRepo
	case !plugin.Enabled:
		row.state, row.detail = stateWarn, "deshabilitado"
		row.command = "claude plugin enable " + claudecli.PluginID
	case plugin.Offered != "" && newer(plugin.Offered, plugin.Version):
		row.state, row.detail = stateWarn, version(plugin.Offered)+" disponible"
		row.command = "claude plugin update " + claudecli.PluginID
		if !commands {
			row.detail += " · [2] actualizar"
		}
	default:
		row.state = stateOK
		row.detail = claudecli.PluginID
		if plugin.Agents > 0 {
			row.detail += " · " + plural(plugin.Agents, "agente", "agentes")
		}
	}
	if commands && row.command != "" {
		row.action = row.command
	}
	return row
}

func storageCheck(data Overview) checkRow {
	storage := data.Storage
	where := displayPath(filepath.Dir(storage.Database), 24)
	switch {
	case storage.Err != nil:
		return checkRow{state: stateError, name: "base de datos", value: "error", detail: "no se pudo leer " + where, action: "abre el diagnóstico con [4]"}
	case !storage.Exists:
		return checkRow{state: stateNeutral, name: "base de datos", value: "sin crear", detail: "se crea en la primera sesión"}
	case storage.Schema < storage.Expected:
		return checkRow{state: stateWarn, name: "base de datos", value: "schema v" + itoa(storage.Schema), detail: "migración pendiente a v" + itoa(storage.Expected)}
	default:
		return checkRow{state: stateOK, name: "base de datos", value: "schema v" + itoa(storage.Schema), detail: where + " · " + plural(storage.Memories, "memoria", "memorias")}
	}
}

func syncCheck(data Overview) checkRow {
	sync := data.Sync
	switch {
	case sync.Err != nil:
		return checkRow{state: stateWarn, name: "sync", value: "sin datos", detail: "no se pudo leer el perfil de sync"}
	case !sync.Configured:
		return checkRow{state: stateNeutral, name: "sync", value: "sin configurar", detail: "opcional · respaldo entre equipos"}
	case !sync.Enabled:
		return checkRow{state: stateWarn, name: "sync", value: "pausado", detail: "el perfil existe pero está deshabilitado"}
	case sync.Credential != "available":
		return checkRow{state: stateError, name: "sync", value: "sin credencial", detail: "credencial " + credentialWord(sync.Credential), action: "vuelve a configurar con [3]"}
	default:
		return checkRow{state: stateOK, name: "sync", value: "configurado", detail: "credencial disponible en el keyring"}
	}
}

func credentialWord(status string) string {
	switch status {
	case "missing":
		return "ausente"
	case "unavailable":
		return "no disponible"
	case "invalid":
		return "inválida"
	case "":
		return "desconocida"
	default:
		return status
	}
}

func handoffCheck(data Overview) checkRow {
	if len(data.Handoffs) == 0 {
		return checkRow{state: stateNeutral, name: "handoff", value: "ninguno", detail: "lo crea la primera sesión"}
	}
	last := data.Handoffs[0]
	return checkRow{state: stateNeutral, name: "handoff", value: shortDate(last.Completed), detail: "«" + sanitizeTerminal(firstLine(last.Summary)) + "»"}
}

// overviewChecks are the Inicio ESTADO rows.
func overviewChecks(data Overview, firstUse bool) []checkRow {
	binary := binaryCheck(data)
	binary.name = "binario"
	rows := []checkRow{binary, claudeCheck(data), pluginCheck(data, false), storageCheck(data), syncCheck(data)}
	if firstUse {
		rows[2].detail = "falta marketplace " + claudecli.MarketplaceRepo
		if data.Plugin.MarketplaceAdded {
			rows[2].detail = "marketplace agregado · falta instalar"
		}
	}
	return append(rows, handoffCheck(data))
}

// diagnosisSection is one titled group of Doctor checks.
type diagnosisSection struct {
	label string
	rows  []checkRow
}

func diagnosisSections(d Diagnosis) []diagnosisSection {
	dir := filepath.Dir(d.Storage.Database)
	root := checkRow{state: stateOK, name: displayPath(dir, 14), value: "ok", detail: "escribible"}
	if d.RootWritable != nil {
		root = checkRow{state: stateError, name: displayPath(dir, 14), value: "error", detail: "no escribible", action: "revisa permisos y espacio en " + displayPath(dir, 40)}
	}
	database := checkRow{state: stateOK, name: "memory.db", value: "ok", detail: "íntegra"}
	schema := checkRow{state: stateOK, name: "esquema", value: "v" + itoa(d.Storage.Schema), detail: "sin migraciones pendientes"}
	switch {
	case d.Storage.Err != nil:
		database = checkRow{state: stateError, name: "memory.db", value: "error", detail: "dañada o ilegible", action: "no la borres; sigue «Actualización y migraciones» en docs/memory.md"}
		schema = checkRow{state: stateError, name: "esquema", value: "error", detail: "no se pudo leer (depende de memory.db)"}
	case !d.Storage.Exists:
		database = checkRow{state: stateNeutral, name: "memory.db", value: "sin crear", detail: "se crea en la primera escritura"}
		schema = checkRow{state: stateNeutral, name: "esquema", value: "v" + itoa(d.Storage.Expected), detail: "se aplica al crear la base"}
	case d.Storage.Schema < d.Storage.Expected:
		schema = checkRow{state: stateWarn, name: "esquema", value: "v" + itoa(d.Storage.Schema), detail: "migración pendiente a v" + itoa(d.Storage.Expected), action: "la primera escritura de Claude Code la aplica"}
	}

	policy := checkRow{state: stateNeutral, name: "SessionStart", value: "sin plugin", detail: "se activa al instalar el plugin"}
	switch {
	case d.Plugin.Installed && d.PolicyErr != nil:
		policy = checkRow{state: stateWarn, name: "SessionStart", value: "sin política", detail: "no se encontró policy/manager.md", action: "reinstala el plugin desde Setup"}
	case d.Plugin.Installed && d.PolicyChars > policyLimit:
		policy = checkRow{state: stateWarn, name: "SessionStart", value: "recortada", detail: "política " + thousands(d.PolicyChars) + " chars; se recorta a " + thousands(policyLimit)}
	case d.Plugin.Installed:
		policy = checkRow{state: stateOK, name: "SessionStart", value: "ok", detail: "política " + thousands(d.PolicyChars) + " chars (tope " + thousands(contextLimit) + ")"}
	}
	server := checkRow{state: stateOK, name: "servidor MCP", value: "ok", detail: "memory · " + itoa(d.MCPTools) + " tools · instructions " + thousands(d.MCPInstructions) + " chars"}
	if d.Storage.Err != nil {
		server.state, server.detail = stateWarn, "arranca · las tools fallan hasta reparar memory.db"
	}

	return []diagnosisSection{
		{label: "Almacenamiento", rows: []checkRow{root, database, schema}},
		{label: "Plugin", rows: []checkRow{binaryCheck(d.Overview), pathBinaryCheck(d.PathBinary), claudeCheck(d.Overview), pluginCheck(d.Overview, true)}},
		{label: "Hooks y MCP", rows: []checkRow{policy, server}},
	}
}

// Limits the hook adapter enforces (internal/cli/claudecode.go).
const (
	policyLimit  = 6_000
	contextLimit = 10_000
)

func itoa(n int) string { return thousands(n) }

// pathBinaryCheck reports the vgxness Claude Code will launch.
func pathBinaryCheck(found PathBinary) checkRow {
	switch {
	case found.Path == "":
		return checkRow{state: stateError, name: "vgxness en PATH", value: "no encontrado", detail: "el plugin lo necesita para la memoria", action: "instálalo con brew install uzielvgx/tap/vgxness"}
	case !found.SupportsPlugin:
		return checkRow{state: stateError, name: "vgxness en PATH", value: version(found.Version), detail: displayPath(found.Path, 30) + " · versión anterior al plugin", action: "actualízalo: brew upgrade vgxness o go install github.com/uzielvgx/vgxness/cmd/vgxness@latest"}
	default:
		return checkRow{state: stateOK, name: "vgxness en PATH", value: version(found.Version), detail: displayPath(found.Path, 30)}
	}
}
