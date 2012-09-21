package cloudinit_test

import (
	"encoding/base64"
	. "launchpad.net/gocheck"
	"launchpad.net/goyaml"
	"launchpad.net/juju-core/environs/cloudinit"
	"launchpad.net/juju-core/environs/config"
	"launchpad.net/juju-core/state"
	"launchpad.net/juju-core/version"
	"regexp"
	"strings"
)

// Use local suite since this file lives in the ec2 package
// for testing internals.
type cloudinitSuite struct{}

var _ = Suite(cloudinitSuite{})

var envConfig = mustNewConfig(map[string]interface{}{
	"type":            "ec2",
	"name":            "foo",
	"default-series":  "series",
	"authorized-keys": "keys",
})

func mustNewConfig(m map[string]interface{}) *config.Config {
	cfg, err := config.New(m)
	if err != nil {
		panic(err)
	}
	return cfg
}

// Each test gives a cloudinit config - we check the
// output to see if it looks correct.
var cloudinitTests = []cloudinit.MachineConfig{
	{
		InstanceIdAccessor: "$instance_id",
		MachineId:          0,
		ProviderType:       "ec2",
		Provisioner:        true,
		AuthorizedKeys:     "sshkey1",
		Tools:              newSimpleTools("1.2.3-linux-amd64"),
		StateServer:        true,
		Config:             envConfig,
		DataDir:            "/var/lib/juju",
	},
	{
		MachineId:      99,
		ProviderType:   "ec2",
		Provisioner:    false,
		AuthorizedKeys: "sshkey1",
		DataDir:        "/var/lib/juju",
		StateServer:    false,
		Tools:          newSimpleTools("1.2.3-linux-amd64"),
		StateInfo:      &state.Info{Addrs: []string{"zk1"}},
	},
}

func newSimpleTools(vers string) *state.Tools {
	return &state.Tools{
		URL:    "http://foo.com/tools/juju" + vers + ".tgz",
		Binary: version.MustParseBinary(vers),
	}
}

// cloundInitTest runs a set of tests for one of the MachineConfig
// values above.
type cloudinitTest struct {
	x   map[interface{}]interface{} // the unmarshalled YAML.
	cfg *cloudinit.MachineConfig    // the config being tested.
}

func (t *cloudinitTest) check(c *C, cfg *cloudinit.MachineConfig) {
	c.Check(t.x["apt_upgrade"], Equals, true)
	c.Check(t.x["apt_update"], Equals, true)
	t.checkScripts(c, "mkdir -p "+cfg.DataDir)
	t.checkScripts(c, "wget.*"+regexp.QuoteMeta(t.cfg.Tools.URL)+".*tar .*xz")

	if t.cfg.StateServer {
		// TODO(dfc) remove this after the switch to mstate
		t.checkPackage(c, "zookeeperd")
		t.checkPackage(c, "mongodb-server")
		t.checkScripts(c, "jujud bootstrap-state")
		t.checkScripts(c, regexp.QuoteMeta(t.cfg.InstanceIdAccessor))
	}
	if t.cfg.Config != nil {
		t.checkEnvConfig(c)
	}
	t.checkPackage(c, "libzookeeper-mt2")
	t.checkPackage(c, "git")

	if t.cfg.Provisioner {
		t.checkScripts(c, "jujud provisioning --state-servers 'localhost"+cloudinit.MgoPortSuffix+"'")
	}

	if t.cfg.StateServer {
		t.checkScripts(c, "jujud machine --state-servers 'localhost"+cloudinit.MgoPortSuffix+"' .* --machine-id [0-9]+")
	} else {
		t.checkScripts(c, "jujud machine --state-servers '"+strings.Join(t.cfg.StateInfo.Addrs, ",")+"' .* --machine-id [0-9]+")
	}
}

// check that any --env-config $base64 is valid and matches t.cfg.Config
func (t *cloudinitTest) checkEnvConfig(c *C) {
	scripts0 := t.x["runcmd"]
	if scripts0 == nil {
		c.Errorf("cloudinit has no entry for runcmd")
		return
	}
	scripts := scripts0.([]interface{})
	re := regexp.MustCompile(`--env-config '([\w,=]+)'`)
	found := false
	for _, s0 := range scripts {
		m := re.FindStringSubmatch(s0.(string))
		if m == nil {
			continue
		}
		found = true
		buf, err := base64.StdEncoding.DecodeString(m[1])
		c.Assert(err, IsNil)
		var actual map[string]interface{}
		err = goyaml.Unmarshal(buf, &actual)
		c.Assert(err, IsNil)
		c.Assert(t.cfg.Config.AllAttrs(), DeepEquals, actual)
	}
	c.Assert(found, Equals, true)
}

func (t *cloudinitTest) checkScripts(c *C, pattern string) {
	CheckScripts(c, t.x, pattern, true)
}

// If match is true, CheckScripts checks that at least one script started
// by the cloudinit data matches the given regexp pattern, otherwise it
// checks that no script matches.  It's exported so it can be used by tests
// defined in ec2_test.
func CheckScripts(c *C, x map[interface{}]interface{}, pattern string, match bool) {
	scripts0 := x["runcmd"]
	if scripts0 == nil {
		c.Errorf("cloudinit has no entry for runcmd")
		return
	}
	scripts := scripts0.([]interface{})
	re := regexp.MustCompile(pattern)
	found := false
	for _, s0 := range scripts {
		s := s0.(string)
		if re.MatchString(s) {
			found = true
		}
	}
	switch {
	case match && !found:
		c.Errorf("script %q not found in %q", pattern, scripts)
	case !match && found:
		c.Errorf("script %q found but not expected in %q", pattern, scripts)
	}
}

func (t *cloudinitTest) checkPackage(c *C, pkg string) {
	CheckPackage(c, t.x, pkg, true)
}

// CheckPackage checks that the cloudinit will or won't install the given
// package, depending on the value of match.  It's exported so it can be
// used by tests defined outside the ec2 package.
func CheckPackage(c *C, x map[interface{}]interface{}, pkg string, match bool) {
	pkgs0 := x["packages"]
	if pkgs0 == nil {
		if match {
			c.Errorf("cloudinit has no entry for packages")
		}
		return
	}

	pkgs := pkgs0.([]interface{})

	found := false
	for _, p0 := range pkgs {
		p := p0.(string)
		if p == pkg {
			found = true
		}
	}
	switch {
	case match && !found:
		c.Errorf("package %q not found in %v", pkg, pkgs)
	case !match && found:
		c.Errorf("%q found but not expected in %v", pkg, pkgs)
	}
}

// TestCloudInit checks that the output from the various tests
// in cloudinitTests is well formed.
func (cloudinitSuite) TestCloudInit(c *C) {
	for i, cfg := range cloudinitTests {
		c.Logf("test %d", i)
		ci, err := cloudinit.New(&cfg)
		c.Assert(err, IsNil)
		c.Check(ci, NotNil)

		// render the cloudinit config to bytes, and then
		// back to a map so we can introspect it without
		// worrying about internal details of the cloudinit
		// package.

		data, err := ci.Render()
		c.Assert(err, IsNil)

		x := make(map[interface{}]interface{})
		err = goyaml.Unmarshal(data, &x)
		c.Assert(err, IsNil)

		t := &cloudinitTest{
			cfg: &cfg,
			x:   x,
		}
		t.check(c, &cfg)
	}
}

// When mutate is called on a known-good MachineConfig,
// there should be an error complaining about the missing
// field named by the adjacent err.
var verifyTests = []struct {
	err    string
	mutate func(*cloudinit.MachineConfig)
}{
	{"negative machine id", func(cfg *cloudinit.MachineConfig) { cfg.MachineId = -1 }},
	{"missing provider type", func(cfg *cloudinit.MachineConfig) { cfg.ProviderType = "" }},
	{"missing instance id accessor", func(cfg *cloudinit.MachineConfig) {
		cfg.InstanceIdAccessor = ""
	}},
	{"missing environment configuration", func(cfg *cloudinit.MachineConfig) {
		cfg.Config = nil
	}},
	{"missing zookeeper hosts", func(cfg *cloudinit.MachineConfig) {
		cfg.StateServer = false
		cfg.StateInfo = nil
	}},
	{"missing zookeeper hosts", func(cfg *cloudinit.MachineConfig) {
		cfg.StateServer = false
		cfg.StateInfo = &state.Info{}
	}},
	{"missing var directory", func(cfg *cloudinit.MachineConfig) {
		cfg.DataDir = ""
		cfg.StateInfo = &state.Info{}
	}},
	{"missing tools", func(cfg *cloudinit.MachineConfig) {
		cfg.Tools = nil
		cfg.StateInfo = &state.Info{}
	}},
	{"missing tools URL", func(cfg *cloudinit.MachineConfig) {
		cfg.Tools = &state.Tools{}
		cfg.StateInfo = &state.Info{}
	}},
}

// TestCloudInitVerify checks that required fields are appropriately
// checked for by NewCloudInit.
func (cloudinitSuite) TestCloudInitVerify(c *C) {
	cfg := &cloudinit.MachineConfig{
		Provisioner:        true,
		StateServer:        true,
		InstanceIdAccessor: "$instance_id",
		ProviderType:       "ec2",
		MachineId:          99,
		Tools:              newSimpleTools("9.9.9-linux-arble"),
		AuthorizedKeys:     "sshkey1",
		StateInfo:          &state.Info{Addrs: []string{"zkhost"}},
		Config:             envConfig,
		DataDir:            "/var/lib/juju",
	}
	// check that the base configuration does not give an error
	_, err := cloudinit.New(cfg)
	c.Assert(err, IsNil)

	for _, test := range verifyTests {
		cfg1 := *cfg
		test.mutate(&cfg1)
		t, err := cloudinit.New(&cfg1)
		c.Assert(err, ErrorMatches, "invalid machine configuration: "+test.err)
		c.Assert(t, IsNil)
	}
}
