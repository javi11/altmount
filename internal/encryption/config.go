package encryption

// CipherType represents the type of encryption cipher used
type CipherType string

const (
	// RCloneCipherType is the rclone crypt cipher type, which encrypts files using a password and salt
	RCloneCipherType CipherType = "rclone"
	// NoneCipherType represents no encryption
	NoneCipherType CipherType = "none"
	// AesCipherType is for AES-CBC encrypted archives (RAR, 7z, etc.)
	AesCipherType CipherType = "aes"
)

// Config contains encryption configuration
type Config struct {
	// Rclone password for the files in case they were encrypted by rclone crypt
	// Use it, in case you don't want to use rclone crypt anymore
	RclonePassword string `yaml:"rclone_password" mapstructure:"rclone_password" json:"-"`
	// Rclone salt for the files in case they were encrypted by rclone crypt
	// Use it, in case you don't want to use rclone crypt anymore
	RcloneSalt string `yaml:"rclone_salt" mapstructure:"rclone_salt" json:"-"`
}
