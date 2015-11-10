# Diego Upgrade Stability Tests (DUSTs)

This test suite exercises the upgrade path from the stable CF/Diego configuration to a current CF/Diego configuration.

## Usage

### Upload the necessary legacy releases to your bosh-lite

```
bosh upload release https://bosh.io/d/github.com/cloudfoundry/cf-release?v=220
bosh upload release https://bosh.io/d/github.com/cloudfoundry-incubator/diego-release?v=0.1434.0
bosh upload release https://bosh.io/d/github.com/cloudfoundry-incubator/garden-linux-release?v=0.307.0
```

### Deploying V0 (legacy)

The DUSTs require the CONFIG environment variable to be set to the path of a valid configuration JSON file.
The following commands will setup the `CONFIG` for a [bosh-lite](https://github.com/cloudfoundry/bosh-lite) installation.
Replace credentials and URLs as appropriate for your environment.

```bash
cat > config.json <<EOF
{
  "bosh_director_url": "192.168.50.4",
  "bosh_admin_user": "admin",
  "bosh_admin_password": "admin",
  "v0_cf_release_path": "../v0-stable-cf-release",
  "v0_diego_release_path": "../v0-stable-diego-release"
  "v1_cf_release_path": "../v1-cf-release",
  "v1_diego_release_path": "../v1-diego-release"
}
EOF
export CONFIG=$PWD/config.json
```

The DUSTs require that your local diego and cf releases be checked out to the stable versions.
This can be done by running the following:

```
pushd ../diego-release
  git co v0.1434.0
  ./scripts/update
popd

pushd ../cf-release
  git co v220
  ./scripts/update
popd
```

You can then run the following tests with:

```bash
ginkgo -v -focus="V0"
```

### Deploying V-prime

The DUSTs require that your local diego and cf releases be checked out to the desired V-prime versions.
Deploying V-prime deploys the latest release on your bosh-lite, make sure to upload the desired cf, diego, garden-linux, and etcd releases.

```
pushd ../diego-release
  git co v0.1439.0
  ./scripts/update
popd

pushd ../cf-release
  git co v223
  ./scripts/update
popd
```

You can then run the following tests with:

```bash
ginkgo -v -focus="V-prime"
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

