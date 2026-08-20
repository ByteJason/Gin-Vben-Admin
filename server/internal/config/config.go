package config

// Config contains the dependency-free runtime settings used by the B1 HTTP seam.
type Config struct {
	Server ServerConfig `yaml:"server"`
}

type ServerConfig struct {
	Addr            string `yaml:"addr"`
	ReadTimeout     string `yaml:"read_timeout"`
	WriteTimeout    string `yaml:"write_timeout"`
	ShutdownTimeout string `yaml:"shutdown_timeout"`
}

func Default() Config {
	return Config{Server: ServerConfig{
		Addr:            ":8080",
		ReadTimeout:     "10s",
		WriteTimeout:    "10s",
		ShutdownTimeout: "10s",
	}}
}
