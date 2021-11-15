package main

// Agent Command-Line Interface (CLI).
//
// The CLI provides just enough features to use as a Docker image's entry point
// and run our service, parse flags, and then configure and run the agent.
//
// We use the Cobra library to handle commands and flags because it works well
// for creating both simple CLIs and complex applications. And Cobra integrates
// with a library called Viper, which is a complete configuration solution for
// Go applications.

import (
	"log"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/cedrickchee/commitlog/internal/agent"
	"github.com/cedrickchee/commitlog/internal/config"
)

// cli struct is a place we put logic and data that's common to all the
// commands.
type cli struct {
	cfg cfg
}

// cfg is a separate struct from the `agent.Config` struct to handle the field
// types that we can't parse without error handling.
type cfg struct {
	agent.Config
	ServerTLSConfig config.TLSConfig
	PeerTLSConfig   config.TLSConfig
}

// setupFlags declares and exposes CLI's flags.
func setupFlags(cmd *cobra.Command) error {
	// These flags allow people calling your CLI to configure the agent and
	// learn the default configuration.

	// We've given usable defaults for the configurations we have to set: the
	// data directory, bind address, the RPC port, and the node name. Try to set
	// usable default flag values instead of requiring users to set them.

	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}

	cmd.Flags().String("config-file", "", "Path to config file.")

	dataDir := path.Join(os.TempDir(), "commitlog")
	cmd.Flags().String("data-dir",
		dataDir,
		"Directory to store log and Raft data.")
	cmd.Flags().String("node-name", hostname, "Unique server ID.")

	// Serf, Raft, gRPC flags.
	cmd.Flags().String("bind-addr", "127.0.0.1:8401",
		"Address to bind Serf on.")
	cmd.Flags().Int("rpc-port", 8400,
		"Port for RPC clients (and Raft) connections.")
	cmd.Flags().StringSlice("start-join-addrs", nil, "Serf addresses to join.")
	cmd.Flags().Bool("bootstrap", false, "Bootstrap the cluster.")

	// Auth - ACL flags.
	cmd.Flags().String("acl-model-file", "", "Path to ACL model.")
	cmd.Flags().String("acl-policy-file", "", "Path to ACL policy.")

	// TLS - server flags.
	cmd.Flags().String("server-tls-cert-file", "", "Path to server tls cert.")
	cmd.Flags().String("server-tls-key-file", "", "Path to server tls key.")
	cmd.Flags().String("server-tls-ca-file", "",
		"Path to server certificate authority.")

	// TLS - client flags.
	cmd.Flags().String("peer-tls-cert-file", "", "Path to peer tls cert.")
	cmd.Flags().String("peer-tls-key-file", "", "Path to peer tls key.")
	cmd.Flags().String("peer-tls-ca-file", "",
		"Path to peer certificate authority.")

	return viper.BindPFlags(cmd.Flags())
}

// setupConfig reads the configuration and prepares the agent's configuration.
func (c *cli) setupConfig(cmd *cobra.Command, args []string) error {
	var err error

	// We could allow users to set the configuration with flags or a file. With
	// a configuration file, we can support dynamic config changes to a running
	// service. A configuration file also enables other processes to set up the
	// configuration for the service.
	configFile, err := cmd.Flags().GetString("config-file")
	if err != nil {
		return err
	}
	viper.SetConfigFile(configFile)

	// Read in the configuration from a file, too, for dynamic configurations.
	if err = viper.ReadInConfig(); err != nil {
		// It's ok if config file doesn't exist.
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return err
		}
	}

	// Viper provides a centralized config registry system where multiple
	// configuration sources can set the configuration but you can read the
	// result in one place.
	c.cfg.DataDir = viper.GetString("data-dir")
	c.cfg.NodeName = viper.GetString("node-name")
	c.cfg.BindAddr = viper.GetString("bind-addr")
	c.cfg.RPCPort = viper.GetInt("rpc-port")
	c.cfg.StartJoinAddrs = viper.GetStringSlice("start-join-addrs")
	c.cfg.Bootstrap = viper.GetBool("bootstrap")
	c.cfg.ACLModelFile = viper.GetString("acl-mode-file")
	c.cfg.ACLPolicyFile = viper.GetString("acl-policy-file")
	c.cfg.ServerTLSConfig.CertFile = viper.GetString("server-tls-cert-file")
	c.cfg.ServerTLSConfig.KeyFile = viper.GetString("server-tls-key-file")
	c.cfg.ServerTLSConfig.CAFile = viper.GetString("server-tls-ca-file")
	c.cfg.PeerTLSConfig.CertFile = viper.GetString("peer-tls-cert-file")
	c.cfg.PeerTLSConfig.KeyFile = viper.GetString("peer-tls-key-file")
	c.cfg.PeerTLSConfig.CAFile = viper.GetString("peer-tls-ca-file")

	if c.cfg.ServerTLSConfig.CertFile != "" &&
		c.cfg.ServerTLSConfig.KeyFile != "" {
		c.cfg.ServerTLSConfig.Server = true
		c.cfg.Config.ServerTLSConfig, err = config.SetupTLSConfig(
			c.cfg.ServerTLSConfig,
		)
		if err != nil {
			return err
		}
	}

	if c.cfg.PeerTLSConfig.CertFile != "" &&
		c.cfg.PeerTLSConfig.KeyFile != "" {
		c.cfg.Config.PeerTLSConfig, err = config.SetupTLSConfig(
			c.cfg.PeerTLSConfig,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// run runs our executable's logic.
func (c *cli) run(cmd *cobra.Command, args []string) error {
	var err error

	// Create the agent.
	agent, err := agent.New(c.cfg.Config)
	if err != nil {
		return err
	}

	// Handling signals from the OS.
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	<-sigc

	// Shutdown the agent gracefully when the OS terminates the program.
	return agent.Shutdown()
}

func main() {
	cli := &cli{}

	// Define our sole command. Our CLI is about as simple as it gets. In more
	// complex applications, this command would act as the root command tying
	// together your subcommands. Cobra calls the `RunE` function you set on
	// your command when the command runs. Put or call the command's primary
	// logic in that function. Cobra enables you to run hook functions to run
	// before and after `RunE`.
	//
	// Cobra provides persistent flags and hooks for applications with many
	// subcommands (so we're not using them in our program)--persistent flags
	// and hooks apply to the current command and all its children.
	cmd := &cobra.Command{
		Use: "commitlog",
		// Cobra calls setupConfig before running the command's RunE function.
		PreRunE: cli.setupConfig,
		RunE:    cli.run,
	}

	// Set up CLI's flags.
	if err := setupFlags(cmd); err != nil {
		log.Fatal(err)
	}

	// Execute the root command to parse the process's arguments and search
	// through the command tree to find the correct command to run. We just have
	// the one command, so we're not making Cobra work hard.
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
