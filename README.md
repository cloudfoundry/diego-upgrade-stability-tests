# Diego Upgrade Stability Tests (DUSTs)

This test suite exercises the upgrade path from the stable CF/Diego configuration to a current CF/Diego configuration.

## Usage

### Upload the necessary legacy releases to your bosh-lite

```
bosh upload release https://bosh.io/d/github.com/cloudfoundry/cf-release?v=220
bosh upload release https://bosh.io/d/github.com/cloudfoundry-incubator/diego-release?v=0.1434.0
bosh upload release https://bosh.io/d/github.com/cloudfoundry-incubator/garden-linux-release?v=0.307.0
```

### Checkout the correct version of legacy releases

```
cd ~/workspace/cf-release-v0
git checkout v220
cd ~/workspace/diego-release-v0
git checkout 0.1434.0
```

### Upload the necessary V-prime releases to your bosh-lite

```
cd ~/workspace/cf-release
git co runtime-passed
bosh create release --force && bosh -n upload release
cd ~/workspace/diego-release
git co develop
bosh create release --force && bosh -n upload release
```

### Upload the necessary stemcell to your bosh-lite

```
bosh upload stemcell https://bosh.io/d/stemcells/bosh-warden-boshlite-ubuntu-trusty-go_agent?v=2776
```

### Run the test suite

The DUSTs require the CONFIG environment variable to be set to the path of a valid configuration JSON file.
The following commands will setup the `CONFIG` for a [bosh-lite](https://github.com/cloudfoundry/bosh-lite) installation.
Replace credentials and URLs as appropriate for your environment.

```bash
cat > config.json <<EOF
{
  "bosh_director_url": "192.168.50.4",
  "bosh_admin_user": "admin",
  "bosh_admin_password": "admin",
  "v0_cf_release_path": "../cf-release-v0",
  "v0_diego_release_path": "../diego-release-v0",
  "v1_cf_release_path": "../cf-release",
  "v1_diego_release_path": "../diego-release"
}
EOF
export CONFIG=$PWD/config.json
```

You can then run the following tests with:

```bash
ginkgo -v
```

