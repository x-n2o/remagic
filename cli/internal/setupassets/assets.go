// Package setupassets contains device-side setup steps shared by both installers.
package setupassets

import _ "embed"

//go:embed rebuild-hashtable.sh
var RebuildHashtable string

//go:embed start-xovi.sh
var StartXovi string

//go:embed install-appload.sh
var InstallAppload string
