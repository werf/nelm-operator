# Changelog

## 1.0.0-alpha.1 (2026-10-09)


### Features

* add charts to deploy nelm-source-controller and 3p-helm-controller ([#1](https://github.com/werf/nelm-operator/issues/1)) ([6a4dda6](https://github.com/werf/nelm-operator/commit/6a4dda6e68ad54999339f783bdd27e79143efc72))
* add serviceAccountName impersanate support ([#8](https://github.com/werf/nelm-operator/issues/8)) ([7a6a06c](https://github.com/werf/nelm-operator/commit/7a6a06c1a98c7e68a3ac666c1394873c0034fb1e))
* add spec.diffPatches ([0141c4f](https://github.com/werf/nelm-operator/commit/0141c4f3ef1c00f498615e80416772fab775d8c8))
* add spec.diffPatches ([e38489f](https://github.com/werf/nelm-operator/commit/e38489fe93a462d3be3d3c94d899d4c711672735))
* add spec.renderPatches support ([#15](https://github.com/werf/nelm-operator/issues/15)) ([a194dd9](https://github.com/werf/nelm-operator/commit/a194dd9ac3e35fa4c00d9dbbfe66c19db57d8ae6))
* adopt the release-please setup used by werf/nelm ([ec25b01](https://github.com/werf/nelm-operator/commit/ec25b018e2db2b6c16d4c0633a39776451923ad4))
* bundle Deno binary into the operator image ([6e77428](https://github.com/werf/nelm-operator/commit/6e774287ab96a3bb6126b4edf7301912451928d2))
* create umbrella nelm-operator chart ([#7](https://github.com/werf/nelm-operator/issues/7)) ([6e81895](https://github.com/werf/nelm-operator/commit/6e81895074f630418a95fc5b2aefc36cbb497119))
* embed kubeconform schemas ([#12](https://github.com/werf/nelm-operator/issues/12)) ([40dcad1](https://github.com/werf/nelm-operator/commit/40dcad1c37cc7e3832e1cb4f1faad1102ddf5b92))
* flatten Release.spec.chart.spec ([1bb324d](https://github.com/werf/nelm-operator/commit/1bb324dc440350780b83e1f8141e69577bd59a3a))
* flatten Release.spec.chart.spec ([4d24fd4](https://github.com/werf/nelm-operator/commit/4d24fd4bf525009e6c8b7256b6dd82beac5c772d))
* implement indexers for sources and config dependencies ([#9](https://github.com/werf/nelm-operator/issues/9)) ([18731f5](https://github.com/werf/nelm-operator/commit/18731f558ff36c8eaac8e2db8f9c4f2cecea9397))
* keep the released version in the repository ([5ea6876](https://github.com/werf/nelm-operator/commit/5ea68769bb204eeab28e6387f8fc73728722e7b7))
* nelm operator v0.1.0 ([4840ace](https://github.com/werf/nelm-operator/commit/4840ace2451c13392d9c416a56716cfe12a96651))
* nelm operator v0.1.0 ([f87e6c6](https://github.com/werf/nelm-operator/commit/f87e6c69894d9431912cecb86bffdacf2a946119))
* publish image, charts and installer from a version tag ([df12fc6](https://github.com/werf/nelm-operator/commit/df12fc6498b4cb3eb16894ad36f5cdec4eb98d07))


### Bug Fixes

* change some keys in spec from `flag: true` to `noFlag: false` by  default ([9713965](https://github.com/werf/nelm-operator/commit/971396511b834b729bad56e15005284d95a18197))
* change some keys in spec from `flag: true` to `noFlag: false` by default ([5e683be](https://github.com/werf/nelm-operator/commit/5e683be967f8ef2c789ad769714a30e0528bab63))
* close the remaining gaps in the release workflows ([5c37e10](https://github.com/werf/nelm-operator/commit/5c37e10cc92f1424c5981c43a2cf97fb9a5d000f))
* correct the concurrency knob and make the umbrella chart self-contained ([f8b872f](https://github.com/werf/nelm-operator/commit/f8b872ff65ca50745f570ed8002da83507c0f478))
* cross-compile the arm64 image instead of emulating it ([a8e93dc](https://github.com/werf/nelm-operator/commit/a8e93dcf0fa8a6f4f5d022ea8753f014bcf1bf7d))
* harden the release pipeline and correct the docs ([77168e9](https://github.com/werf/nelm-operator/commit/77168e9f68bbed28cb9244c301af195a325c9b1c))
* make the linter pass on main ([84cd901](https://github.com/werf/nelm-operator/commit/84cd901d049f6ec5febd19299d364eeef3803ea8))
* optimize local validation args ([#14](https://github.com/werf/nelm-operator/issues/14)) ([fc5607c](https://github.com/werf/nelm-operator/commit/fc5607cab853a3819d7dbbcb108e14ab1eefaad7))
* point the kustomize manifests at the real image repository ([1b467c6](https://github.com/werf/nelm-operator/commit/1b467c6d5a91f499fa0c62c51d943ba570a89625))
* regenerate chart CRD and keep it in sync with the generated one ([0d4e9b9](https://github.com/werf/nelm-operator/commit/0d4e9b9d40d9634291c7844399e32c63164a6965))
* status synced with releases and release ownserhip ([086c80b](https://github.com/werf/nelm-operator/commit/086c80b88eb7b7b4bf903cdea6815399c29c7bbf))
* status synced with releases and release ownserhip ([a5ec0a3](https://github.com/werf/nelm-operator/commit/a5ec0a34e7b294c020e391490ad82744993485b2))


### Miscellaneous Chores

* release 1.0.0-alpha.1 ([0f534ea](https://github.com/werf/nelm-operator/commit/0f534ea8198ae43b3c66884b6166a85262f5ac30))
