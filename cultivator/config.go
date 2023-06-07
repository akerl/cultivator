package cultivator

import (
	"fmt"
	"io/ioutil"

	"github.com/akerl/timber/log"
	"github.com/ghodss/yaml"
)

var logger = log.NewLogger("cultivator")

const defaultConfigFile = "config.yaml"

// Config describes options for changing the behavior of Cultivator
type Config struct {
	CacheDir       string   `json:"cache_dir"`
	IntegrationID  int      `json:"integration_id"`
	PrivateKeyFile string   `json:"private_key_file"`
	Checks         []string `json:"checks"`
}

func loadConfig(fileArg string) (Config, error) {
	var c Config
	var err error

	file := fileArg
	if file == "" {
		file = defaultConfigFile
	}
	logger.DebugMsg(fmt.Sprintf("loading config from %s", file))

	contents, err := ioutil.ReadFile(file)
	if err != nil {
		return c, err
	}

	err = yaml.Unmarshal(contents, &c)
	return c, err
}
