package service

// classMaskLabels is ordered by bit value so decoded strings follow the
// specification's stable order (Design.md L423-L453).
var classMaskLabels = [...]string{
	"WAR", // 1
	"CLR", // 2
	"PAL", // 4
	"RNG", // 8
	"SHD", // 16
	"DRU", // 32
	"MNK", // 64
	"BRD", // 128
	"ROG", // 256
	"SHM", // 512
	"NEC", // 1024
	"WIZ", // 2048
	"MAG", // 4096
	"ENC", // 8192
	"BST", // 16384
	"BER", // 32768
}

// ClassMaskDecoder converts a logical ClassList bitmask into the comma-
// separated class abbreviations stored in GuildBankItems.ClassString. It has
// no repository dependency and can be reused by import and inventory
// workflows.
type ClassMaskDecoder struct{}

// NewClassMaskDecoder builds the class-mask decoder.
func NewClassMaskDecoder() *ClassMaskDecoder {
	return &ClassMaskDecoder{}
}

// Decode applies PD-19/PD-20 without failing on undefined high bits.
func (d *ClassMaskDecoder) Decode(mask uint64) MaskDecodeResult {
	return decodeMask(mask, classMaskLabels[:], "ClassList")
}
