package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"
	"sync"

	go_scp "github.com/bramvdbogaerde/go-scp"
	"github.com/go-yaml/yaml"
	"golang.org/x/crypto/ssh"
)

func main() {
	fmt.Println("Whiskey Deploy")

	// Initial command line verification
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <config>", os.Args[0])
	}

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
		client, session, scpcli, err := connect(target)
		if err != nil {
			log.Fatalf("Can't connect: %v", err)
		}
		fmt.Printf("Connected...\n")

		// Close the client at the end of the block
		defer client.Close()

		// export TEMPFILE=$(ssh $SSH_HOST "mktemp -d")
		tmpfilestream, err := session.CombinedOutput("mktemp -d")
		if err != nil {
			panic(err)
		}
		tmpfile := strings.TrimSpace(string(tmpfilestream))
		log.Println(string(tmpfile))
		session.Close()

		//scp whiskey_remote.sh $ARTIFACTS $SSH_HOST:$TEMPFILE

		// Connect to the remote server
		err = scpcli.Connect()
		if err != nil {
			fmt.Println("Couldn't establish a connection to the remote server ", err)
			return
		}

		defer scpcli.Close()
		log.Println(scpcli.RemoteBinary)
		scpcli.RemoteBinary = "/usr/bin/scp"

		// This program
		progname, err := os.Executable()
		if err != nil {
			panic(err)
		}
		fmt.Printf("Copying executable %s...", path.Base(progname))
		prog, err := os.Open(progname)
		if err != nil {
			panic(err)
		}
		scpcli.CopyFromFile(*prog, string(tmpfile)+"/"+path.Base(progname), "0755")
		// We don't check for errors from the above command because there is an odd 127 error code
		fmt.Println(" DONE")
		scpcli.Close()

		// Config file
		fmt.Printf("Copying config file %s...", os.Args[1])
		scpcli.Connect()
		conf, _ := os.Open(os.Args[1])
		err = scpcli.CopyFromFile(*conf, string(tmpfile)+"/"+path.Base(os.Args[1]), "0644")
		// We don't check for errors from the above command because there is an odd 127 error code
		fmt.Println(" DONE")

		//TODO: Copy artifacts

		//ssh $SSH_HOST "cd $TEMPFILE && bash whiskey_remote.sh ; rm -rf $TEMPFILE"
		session, _ = client.NewSession()
		in, err := session.StdinPipe()
		if err != nil {
			fmt.Printf("ERROR Opening Stdin: %v\n", err)
		}
		out, err := session.StdoutPipe()
		if err != nil {
			fmt.Printf("ERROR Opening Stdout: %v\n", err)
		}
		errs, err := session.StderrPipe()
		if err != nil {
			fmt.Printf("ERROR Opening Stderr: %v\n", err)
		}
		err = session.Shell()
		if err != nil {
			fmt.Printf("ERROR Opening Shell: %v\n", err)
		}
		fmt.Printf("Shell opened\n")

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			cmd := fmt.Sprintf("cd %s && ./%s --remote %s ; rm -rf %s ; exit\n", tmpfile, path.Base(progname), path.Base(os.Args[1]), tmpfile)
			fmt.Print("Running command: " + cmd)
			_, err = in.Write([]byte(cmd))
			if err != nil {
				fmt.Printf("ERROR Writing command to shell: %v\n", err)
			}
		}()

		go func() {
			defer wg.Done()
			_, err = io.Copy(os.Stdout, out)
			if err != nil {
				fmt.Printf("ERROR writing output: %v\n", err)
			}
		}()

		go func() {
			defer wg.Done()
			_, err = io.Copy(os.Stderr, errs)
			if err != nil {
				fmt.Printf("ERROR writing stderr: %v\n", err)
			}
		}()

		wg.Wait()

		session.Close()

		client.Close()
	}
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
	yamlFile, err := ioutil.ReadFile(os.Args[1])
	if err != nil {
		return nil, err
	}
	cfg := new(whiskeyConfig)
	err = yaml.Unmarshal(yamlFile, cfg)
	return cfg, err
}

func connect(connstr string) (*ssh.Client, *ssh.Session, *go_scp.Client, error) {
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
		return nil, nil, nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, nil, nil, err
	}

	scpcli := go_scp.NewClient(host+":22", sshConfig)

	return client, session, &scpcli, nil
}
