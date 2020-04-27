package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/go-yaml/yaml"
	"golang.org/x/crypto/ssh"
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
		client, session, err := connect(target)
		if err != nil {
			log.Fatalf("Can't connect: %v", err)
		}
		fmt.Printf("Connected...\n")

		// Close the client at the end of the block
		defer client.Close()

		out, err := session.CombinedOutput(os.Args[3])
		if err != nil {
			panic(err)
		}
		fmt.Println(string(out))
		client.Close()
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

func connect(connstr string) (*ssh.Client, *ssh.Session, error) {
	var user, host, path string

	tmp := strings.Split(connstr, ":")
	path = tmp[1]
	tmp2 := strings.Split(tmp[0], "@")
	user = tmp2[0]
	host = tmp2[1]

	log.Printf("User: %s", user)
	log.Printf("Host: %s", host)
	log.Printf("Path: %s", path)

	key := os.Getenv("SCP_PRIVATE_KEY")
	if key == "" {
		log.Fatalf("Please ensure the private key contents are present in $SCP_PRIVATE_KEY")
	}

	signer, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		log.Fatalf("Unable to parse private key: %v", err)
	}

	sshConfig := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
	}
	sshConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()

	client, err := ssh.Dial("tcp", host+":22", sshConfig)
	if err != nil {
		return nil, nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, nil, err
	}

	return client, session, nil
}
