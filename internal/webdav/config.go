package webdav

type Config struct {
	// Port is the port where the webdav server will be listening
	Port int `yaml:"port" default:"8080" mapstructure:"port"`
	// User is the user to access the webdav server
	User string `yaml:"username" default:"usenet" json:"-" mapstructure:"username"`
	// Pass is the password to access the webdav server
	Pass string `yaml:"password" default:"usenet" json:"-" mapstructure:"password"`
	// Prefix is the URL path prefix for the WebDAV server
	Prefix string `yaml:"prefix" default:"/webdav/" mapstructure:"prefix"`
}
