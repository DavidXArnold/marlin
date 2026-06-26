package configs

import "embed"

// Models embeds all bundled model profiles. The binary always carries these
// so abstract base profiles (e.g. nvfp4-base) are resolvable even when not
// present in any of the user model directories on disk.
//
//go:embed models
var Models embed.FS
