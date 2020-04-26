package main

import (
	"fmt"
	"io/ioutil"
	"log"

	"github.com/go-yaml/yaml"
)

func main() {
	fmt.Println("Whiskey Deploy")
	if shouldRunRemote() {
		runRemote()
	} else {
		runLocal()
	}
}

func shouldRunRemote() bool {
	return false
}

func runRemote() {
	fmt.Println("Running Remote tasks")

	// Unpack files
	// tar zxf PhotoHub-Linux-*.tar.gz

	// NEW=$(date +'%s')

	// Copy files [copy]

	// Build [build]

	// Switch symlink
	// ln -sfn $NEW $DEPLOY_BASE/Current

	// Postinst [postinst]

	// Restart/reload the app [restart]

	// And clean up old deployments
	// cd $DEPLOY_BASE/
	// ls -1tr | grep -v Current | head -n -5 | xargs -r rm -r
}

func runLocal() {
	fmt.Println("Running Local tasks")

	cfg, err := getConfig()
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	for _, target := range cfg.Targets {
		fmt.Printf("Deploying to target: %v\n", target)
	}

	// export TEMPFILE=$(ssh $SSH_HOST "mktemp -d")

	//scp whiskey_remote.sh $ARTIFACTS $SSH_HOST:$TEMPFILE
	//ssh $SSH_HOST "cd $TEMPFILE && bash whiskey_remote.sh ; rm -rf $TEMPFILE"
}

type whiskeyConfig struct {
	Artifacts []string
	Targets   []string
	Copy      []string
	Build     []string
	Postinst  []string
	Restart   []string
}

func getConfig() (*whiskeyConfig, error) {
	yamlFile, err := ioutil.ReadFile("example.whiskey-cd.yml")
	if err != nil {
		return nil, err
	}
	cfg := new(whiskeyConfig)
	err = yaml.Unmarshal(yamlFile, cfg)
	return cfg, err
}
