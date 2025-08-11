package webdav

type Config struct {
	// Port is the port where the webdav server will be listening
	Port int `yaml:"port" default:"8080" mapstructure:"port"`
	// User is the user to access the webdav server
	User string `yaml:"username" default:"usenet" json:"-" mapstructure:"username"`
	// Pass is the password to access the webdav server
	Pass string `yaml:"password" default:"usenet" json:"-" mapstructure:"password"`
	// Debug enables debug mode and exposes profiler endpoints
	Debug bool `yaml:"debug" default:"false" mapstructure:"debug"`
}
