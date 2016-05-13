# Diego Upgrade Stability Tests (DUSTs)

This test suite exercises the upgrade path from the stable CF/Diego configuration to a current CF/Diego configuration.

## Usage

### Upload the necessary legacy releases to your bosh-lite

```bash
# Legacy Releases
bosh upload release https://bosh.io/d/github.com/cloudfoundry/cf-release?v=220
bosh upload release https://bosh.io/d/github.com/cloudfoundry-incubator/diego-release?v=0.1434.0
bosh upload release https://bosh.io/d/github.com/cloudfoundry-incubator/garden-linux-release?v=0.307.0

# Current Releases
bosh upload release https://bosh.io/d/github.com/cloudfoundry-incubator/etcd-release
bosh upload release https://bosh.io/d/github.com/cloudfoundry-incubator/garden-linux-release
bosh upload release https://bosh.io/d/github.com/cloudfoundry/cflinuxfs2-rootfs-release
```

### Checkout the correct version of legacy releases

The V0 manifest generation depends on having cf-release and diego-release cloned to an additional directory.
The desired versions of each release should be checked out.

```bash
cd ~/workspace/cf-release-v0
git checkout v220
git submodule update --init --recursive src/loggregator # Need manifest generation templates for LAMB
cd ~/workspace/diego-release-v0
git checkout 0.1434.0
```

### Upload the necessary V-prime releases to your bosh-lite

```bash
cd ~/workspace/cf-release
git checkout runtime-passed
./scripts/update
bosh -n --parallel 10 create release --force && bosh upload release
cd ~/workspace/diego-release
git checkout develop
./scripts/update
bosh -n --parallel 10 create release --force && bosh upload release
```

### Upload the necessary stemcell to your bosh-lite

```
bosh upload stemcell https://bosh.io/d/stemcells/bosh-warden-boshlite-ubuntu-trusty-go_agent
```

### Run the test suite

The DUSTs require the CONFIG environment variable to be set to the path of a valid configuration JSON file.
The following commands will setup the `CONFIG` for a [bosh-lite](https://github.com/cloudfoundry/bosh-lite) installation.
Replace credentials and URLs as appropriate for your environment.

```bash
cat > config.json <<EOF
{
  "override_domain": "bosh-lite.com",
  "bosh_director_url": "192.168.50.4",
  "bosh_admin_user": "admin",
  "bosh_admin_password": "admin",
  "base_directory": "$HOME/workspace/",
  "v0_cf_release_path": "[LEGACY CF RELEASE DIR]",
  "v0_diego_release_path": "[LEGACY DIEGO RELEASE DIR]",
  "v1_cf_release_path": "[CF RELEASE DIR]",
  "v1_diego_release_path": "[DIEGO RELEASE DIR]",
  "max_polling_errors": 1,
  "aws_stubs_directory": REPLACE_ME
}
EOF
export CONFIG=$PWD/config.json
```

Make sure the release directories for the legacy and latest Cloud Foundry and Diego are named `cf-release` and `diego-release`, otherwise the deployments will fail.

The aws_stubs_directory is required due to the fact that bosh-lite has breaking changes to the blobstore
when running locally. Using an AWS s3 bucket allows us to work around this issue.

You can then run the following tests with:

```bash
ginkgo -v
```

