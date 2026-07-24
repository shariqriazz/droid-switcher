package switcher

const (
	appDirName      = ".droid-switcher"
	factoryDirName  = ".factory"
	authFileName    = "auth.v2.file"
	authKeyFileName = "auth.v2.key"
	activeFileName  = "active"
)

var (
	authFiles = []string{authFileName, authKeyFileName}
	seedFiles = []string{"settings.json", "settings-server.json", "mcp.json"}
)
