package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	go_scp "github.com/bramvdbogaerde/go-scp"
	"github.com/go-yaml/yaml"
	"golang.org/x/crypto/ssh"
)

func main() {
	fmt.Println("Whiskey Deploy")

	// Initial command line verification
	if len(os.Args) < 2 || len(os.Args) > 3 {
		log.Fatalf("Usage: %s [--remote] <config>", os.Args[0])
	}

	if shouldRunRemote() {
		runRemote()
	} else {
		runLocal()
	}
}

func shouldRunRemote() bool {
	return os.Args[1] == "--remote"
}

func runRemote() {
	fmt.Println("Running Remote tasks")

	cfg, err := getConfig()
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	// Unpack files
	// tar zxf PhotoHub-Linux-*.tar.gz
	for _, artifact := range cfg.Artifacts {
		matches, err := filepath.Glob(artifact)
		if err != nil {
			fmt.Printf("Could not find %s\n", artifact)
			continue
		}
		if len(matches) == 0 {
			fmt.Printf("Could not find %s\n", artifact)
			continue
		}
		for _, match := range matches {
			fmt.Printf("Unpacking file %s...", match)
			// TODO: Support different file types
			err = unpackTarGz(match)
			if err != nil {
				fmt.Printf("ERROR: %v\n", err)
			} else {
				fmt.Println("DONE")
			}
		}
	}

	fmt.Println("After unpacking, the following files are present: ")
	files, err := ioutil.ReadDir(".")
	if err != nil {
		fmt.Printf("ERROR Listing files %v\n", err)
	} else {
		for _, dir := range files {
			fmt.Println(dir.Name())
		}
	}

	// NEW=$(date +'%s')
	newFolder := strconv.FormatInt(time.Now().Unix(), 10)
	// DEPLOY_DIR=$DEPLOY_BASE/$NEW # Set this in the environment where all scripts are run
	deployDir := path.Join(cfg.DeployBase, newFolder)
	fmt.Printf("Using DEPLOY_DIR=%s\n", deployDir)

	// TODO: For some reason, this isn't working, so let's just use /bin/bash
	// Get the user's default shell
	/*shcommand := exec.Command("getent passwd `id -un` | cut -d: -f7")
	shellstr, err := shcommand.CombinedOutput()
	shell := strings.TrimSpace(string(shellstr))*/
	shell := "/bin/bash"

	fmt.Printf("Running commands using shell %s\n", shell)

	// Copy files [copy]
	fmt.Println("Running copy commands...")
	runCommands(cfg.Copy, shell, fmt.Sprintf("DEPLOY_DIR=%s", deployDir))

	// If $DEPLOY_DIR does not exist after this step OR it is not a directory, deployment fails
	fmt.Println("Verifying existence of new version...")
	if stat, err := os.Stat(deployDir); err != nil || !stat.IsDir() {
		fmt.Printf("Deploy directory %s was not created by 'copy' step\n", deployDir)
		os.Exit(1)
	}

	// Build [build]
	fmt.Println("Running build commands...")
	runCommands(cfg.Build, shell, fmt.Sprintf("DEPLOY_DIR=%s", deployDir))

	// Switch symlink (relative symlink)
	// ln -sfn $NEW $DEPLOY_BASE/Current
	fmt.Println("Updating symlink...")
	runCommands([]string{fmt.Sprintf("ln -sfn %s %s/Current", newFolder, cfg.DeployBase)}, shell)

	// Copy contents of .config to new deploy
	// cp -r $DEPLOY_BASE/.config/* $DEPLOY_BASE/.config/.* $DEPLOY_BASE/Current/
	fmt.Println("Copying environment-specific config, if present")
	configDir := fmt.Sprintf("%s/.config", cfg.DeployBase)
	if stat, err := os.Stat(configDir); err == nil {
		if stat.IsDir() {
			if files, err := ioutil.ReadDir(configDir); err == nil {
				for _, file := range files {
					runCommands([]string{fmt.Sprintf("cp -rp %s/%s %s/Current", configDir, file.Name(), cfg.DeployBase)}, shell)
				}
			}
		} else {
			fmt.Printf("WARNING: %s is not a directory, ignoring", configDir)
			fmt.Println()
		}
	}

	// Postinst [postinst]
	fmt.Println("Running post-install commands...")
	runCommands(cfg.Postinst, shell, fmt.Sprintf("DEPLOY_DIR=%s", deployDir))

	// Restart/reload the app [restart]
	fmt.Println("Running restart commands...")
	runCommands(cfg.Restart, shell, fmt.Sprintf("DEPLOY_DIR=%s", deployDir))

	// And clean up old deployments
	// cd $DEPLOY_BASE/
	// ls -1tr | grep -v Current | head -n -5 | xargs -r rm -r
	fmt.Println("Cleaning up old deployments...")
	runCommands([]string{
		fmt.Sprintf("cd %s", cfg.DeployBase),
		"ls -1tr | grep -v Current | grep -v .config | head -n -5 | xargs -r rm -r"},
		shell)
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
		scpcli.Close()

		// Copy artifacts
		for _, artifact := range cfg.Artifacts {
			matches, err := filepath.Glob(artifact)
			if err != nil {
				fmt.Printf("Could not find %s\n", artifact)
				continue
			}
			if len(matches) == 0 {
				fmt.Printf("Could not find %s\n", artifact)
				continue
			}
			for _, match := range matches {
				fmt.Printf("Copying file %s...", match)
				scpcli.Connect()
				conf, _ := os.Open(match)
				err = scpcli.CopyFromFile(*conf, string(tmpfile)+"/"+path.Base(match), "0644")
				// We don't check for errors from the above command because there is an odd 127 error code
				fmt.Println(" DONE")
				scpcli.Close()
			}
		}

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
			cmd := fmt.Sprintf("cd %s && ./%s --remote %s ; STATUS=$? ; rm -rf %s ; exit $STATUS\n", tmpfile, path.Base(progname), path.Base(os.Args[1]), tmpfile)
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

		if err = session.Wait(); err != nil {
			log.Fatalf("SSH session closed with error %v", err)
		}

		client.Close()
	}
}

type whiskeyConfig struct {
	Artifacts  []string
	Targets    []string
	DeployBase string `yaml:"deploy_base"`
	Copy       []string
	Build      []string
	Postinst   []string
	Restart    []string
}

func getConfig() (*whiskeyConfig, error) {
	var cfgpath string
	if os.Args[1] == "--remote" {
		cfgpath = os.Args[2]
	} else {
		cfgpath = os.Args[1]
	}
	yamlFile, err := ioutil.ReadFile(cfgpath)
	if err != nil {
		return nil, err
	}
	cfg := new(whiskeyConfig)
	err = yaml.Unmarshal(yamlFile, cfg)
	return cfg, err
}

func connect(connstr string) (*ssh.Client, *ssh.Session, *go_scp.Client, error) {
	var user, host string

	tmp := strings.Split(connstr, "@")
	user = tmp[0]
	host = tmp[1]

	log.Printf("User: %s", user)
	log.Printf("Host: %s", host)

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

func unpackTarGz(filename string) error {
	// Pulled pretty much directly from https://stackoverflow.com/questions/57639648/how-to-decompress-tar-gz-file-in-go
	var err error = nil

	gzipStream, err := os.Open(filename)
	if err != nil {
		fmt.Println("Open error")
		return err
	}

	// Why do we need zlib rather than gzip?
	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		fmt.Println("Decompress error")
		return err
	}

	tarReader := tar.NewReader(uncompressedStream)

	for {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			fmt.Println("Read error")
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.Mkdir(header.Name, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			outFile, err := os.Create(header.Name)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				return err
			}
			outFile.Close()

		default:
			log.Fatalf(
				"unpackTarGz: uknown type: %d in %s",
				header.Typeflag,
				header.Name)
		}
	}
	return nil
}

func runCommands(commands []string, shell string, envVars ...string) {
	command := exec.Command(shell)
	command.Env = append(command.Env, envVars...)
	commandStdin, err := command.StdinPipe()
	if err != nil {
		fmt.Println("ERROR Opening Stdin")
	}
	commandStdout, err := command.StdoutPipe()
	if err != nil {
		fmt.Println("ERROR Opening Stdout")
	}
	commandStderr, err := command.StderrPipe()
	if err != nil {
		fmt.Println("ERROR Opening Stderr")
	}
	err = command.Start()
	if err != nil {
		fmt.Println("ERROR Starting shell")
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // Stdin
		commandStdin.Write([]byte("set -ev\n"))
		for _, cmd := range commands {
			commandStdin.Write([]byte(cmd + "\n"))
		}
		commandStdin.Write([]byte("exit\n"))
	}()

	go func() {
		defer wg.Done()
		_, err = io.Copy(os.Stdout, commandStdout)
		if err != nil {
			fmt.Printf("ERROR writing output: %v\n", err)
		}
	}()

	go func() {
		defer wg.Done()
		_, err = io.Copy(os.Stderr, commandStderr)
		if err != nil {
			fmt.Printf("ERROR writing stderr: %v\n", err)
		}
	}()

	wg.Wait()
	if err = command.Wait(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			log.Fatalf("Deployment failed with code %d\n", exiterr.ExitCode())
		}
	}
}
