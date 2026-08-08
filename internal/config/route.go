package config

type RouteConfig struct {
	Name        string `yaml:"name"`
	Protocol    string `yaml:"protocol"`
	PathPrefix  string `yaml:"path_prefix"`
	UpstreamURL string `yaml:"upstream_url"`
}

type fileConfig struct {
	Routes []RouteConfig `yaml:"routes"`
}
