package switcher

const (
	appDirName      = ".droid-switcher"
	factoryDirName  = ".factory"
	authFileName    = "auth.v2.file"
	authKeyFileName = "auth.v2.key"
	// authKeyringFileName is Droid's keyring-v2 credential store. Its AES key
	// lives in the OS keyring instead of next to the file.
	authKeyringFileName = "auth.v2.keyring"
	// authKeyringKeyFileName is switcher-owned: a base64 snapshot of the OS
	// keyring AES key. It is stored only inside saved account homes so quota
	// refreshes and switching work without D-Bus access.
	authKeyringKeyFileName = "auth.v2.keyring.key"
	activeFileName         = "active"
)

var seedFiles = []string{"settings.json", "settings-server.json", "mcp.json"}
