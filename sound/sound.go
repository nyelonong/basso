// Package sound holds the sample kits built into the basso binary.
package sound

import "embed"

// Kit808 holds the 808 samples under the directory "808".
//
//go:embed 808/*.wav
var Kit808 embed.FS
