package archive

import "github.com/javi11/altmount/internal/importer/rarname"

// RAR / split-volume filename helpers re-exported from the dependency-free
// rarname leaf package, which is the single source of truth. They are re-exported
// here because the rar subpackage already depends on archive and references these
// names as archive.* (and the sevenzip package aliases archive.PartPattern etc.).
// The filesystem package imports rarname directly to avoid an import cycle.
var (
	PartPattern    = rarname.PartPattern
	NumericPattern = rarname.NumericPattern
	RPattern       = rarname.RPattern
	RollVolPattern = rarname.RollVolPattern
)

// RarScheme aliases rarname.Scheme so existing rar-package code keeps compiling.
type RarScheme = rarname.Scheme

const (
	SchemeUnknown = rarname.SchemeUnknown
	SchemePart    = rarname.SchemePart
	SchemeRoll    = rarname.SchemeRoll
	SchemeNumeric = rarname.SchemeNumeric
)

// SetKey returns the multi-volume grouping key for a filename. See rarname.SetKey.
func SetKey(filename string) (string, bool) { return rarname.SetKey(filename) }

// VolumeNumber returns the volume scheme and ordinal for a filename. See
// rarname.VolumeNumber.
func VolumeNumber(filename string) (RarScheme, int, bool) { return rarname.VolumeNumber(filename) }
