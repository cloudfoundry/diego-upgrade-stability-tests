# Diego Upgrade Stability Tests (DUSTs)

This test suite exercises the upgrade path from the stable CF/Diego configuration to a current CF/Diego configuration.

## Usage

### Running the Tests

The DUSTs require the CONFIG environment variable to be set to the path of a valid configuration JSON file.
The following commands will setup the `CONFIG` for a [bosh-lite](https://github.com/cloudfoundry/bosh-lite) installation.
Replace credentials and URLs as appropriate for your environment.

```bash
cat > config.json <<EOF
{
  "bosh_director_url": "192.168.50.4",
  "bosh_admin_user": "admin",
  "bosh_admin_password": "admin",
  "cf_release_path": "../cf-release",
  "diego_release_path": "../diego-release"
}
EOF
export CONFIG=$PWD/config.json
```

You can then run the following tests with:

```bash
ginkgo -v
```

### Generating the Manifests

In order to generate manifests for the piecewise deployments using your current workspace, you can run the following:

    ./scripts/generate-manifests -d PATH_TO_DIEGO_RELEASE -c PATH_TO_CF_RELEASE

To generate manifests for the last known stable configuration, you can run the following:

```bash
export DIEGO_RELEASE=PATH_TO_DIEGO_RELEASE
export CF_RELEASE=PATH_TO_DIEGO_RELEASE

pushd $DIEGO_RELEASE
  git co TAG_FOR_LATEST_STABLE_RELEASE
popd

pushd $CF_RELEASE
  git co TAG_FOR_LATEST_STABLE_RELEASE
popd

./scripts/generate-manifests -d $DIEGO_RELEASE -c $CF_RELEASE -l
```

