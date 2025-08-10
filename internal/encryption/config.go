package encryption

type Config struct {
	// Rclone password for the files in case they were encrypted by rclone crypt
	// Use it, in case you don't want to use rclone crypt anymore
	RclonePassword string `yaml:"rclone_password" mapstructure:"rclone_password" json:"-"`
	// Rclone salt for the files in case they were encrypted by rclone crypt
	// Use it, in case you don't want to use rclone crypt anymore
	RcloneSalt string `yaml:"rclone_salt" mapstructure:"rclone_salt" json:"-"`
}
