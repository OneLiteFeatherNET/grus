# Changelog

## 1.0.0 (2026-11-28)


### Features

* **agent:** run instances on hosts outside kubernetes ([#50](https://github.com/OneLiteFeatherNET/grus/issues/50)) ([e6760f6](https://github.com/OneLiteFeatherNET/grus/commit/e6760f68cb52ed7390b07e031b480195bd5e9488))
* **api:** add the v1 rest api with token auth ([#10](https://github.com/OneLiteFeatherNET/grus/issues/10)) ([9d6a8d7](https://github.com/OneLiteFeatherNET/grus/commit/9d6a8d7fb30586c335b2bf8a8467c840d62e94a2))
* **chart:** add an optional web ui deployment ([#33](https://github.com/OneLiteFeatherNET/grus/issues/33)) ([71539fe](https://github.com/OneLiteFeatherNET/grus/commit/71539fe18793e373a5084c964750708d007a37a5))
* **chart:** add the grus helm chart ([#5](https://github.com/OneLiteFeatherNET/grus/issues/5)) ([336adac](https://github.com/OneLiteFeatherNET/grus/commit/336adacd4741ab8f08398dc425ecbf41a86d38ae))
* **chart:** pass agent token hashes to the controller ([#52](https://github.com/OneLiteFeatherNET/grus/issues/52)) ([ccc250b](https://github.com/OneLiteFeatherNET/grus/commit/ccc250b18ede7e530a57f9f375ba51fb809013cd))
* **cli:** add the grus client commands and repl ([#51](https://github.com/OneLiteFeatherNET/grus/issues/51)) ([58123e1](https://github.com/OneLiteFeatherNET/grus/commit/58123e1b430ea7d54ae74505ae4e734f43e65608))
* **cmd:** wire the controller, fetch and openapi commands ([#18](https://github.com/OneLiteFeatherNET/grus/issues/18)) ([1527928](https://github.com/OneLiteFeatherNET/grus/commit/1527928b3af58531d8553d416f4ffb096008eaa8))
* **controller:** add environments, groups and placement to the pool model ([#48](https://github.com/OneLiteFeatherNET/grus/issues/48)) ([0b14fcd](https://github.com/OneLiteFeatherNET/grus/commit/0b14fcda3a6286ef1e503de394b48ba9b0556780))
* **controller:** add instance state machine and autoscaler ([#2](https://github.com/OneLiteFeatherNET/grus/issues/2)) ([9832f8f](https://github.com/OneLiteFeatherNET/grus/commit/9832f8fd3d6df09c6b3e660832247e16814375b9))
* **controller:** add the reconciler and controller service ([#13](https://github.com/OneLiteFeatherNET/grus/issues/13)) ([e4c2888](https://github.com/OneLiteFeatherNET/grus/commit/e4c2888314ee2a836f0084014122f8f03735481f))
* **controller:** allocate private servers per owner ([#55](https://github.com/OneLiteFeatherNET/grus/issues/55)) ([fcafc0d](https://github.com/OneLiteFeatherNET/grus/commit/fcafc0dfcc666757f5279cff82079630763041e5))
* **controller:** assign region cells to instances ([#57](https://github.com/OneLiteFeatherNET/grus/issues/57)) ([82a3d5a](https://github.com/OneLiteFeatherNET/grus/commit/82a3d5acab6709de0de49e86510970708171ce34))
* **controller:** declare environments, groups and pools as kubernetes custom resources ([#49](https://github.com/OneLiteFeatherNET/grus/issues/49)) ([7055f78](https://github.com/OneLiteFeatherNET/grus/commit/7055f78123030e85e6af83c4222519e6efaff4db))
* **driver:** add the fake and local process drivers ([#9](https://github.com/OneLiteFeatherNET/grus/issues/9)) ([6dafd81](https://github.com/OneLiteFeatherNET/grus/commit/6dafd81f03c6732c31683a422372439c33e5a8e0))
* **driver:** add the kubernetes bare pod driver ([#8](https://github.com/OneLiteFeatherNET/grus/issues/8)) ([cea56e9](https://github.com/OneLiteFeatherNET/grus/commit/cea56e9d9f44c1ed4bd3c046aff5ff830901c497))
* **driver:** route instances to several drivers by host ([#41](https://github.com/OneLiteFeatherNET/grus/issues/41)) ([1b14778](https://github.com/OneLiteFeatherNET/grus/commit/1b147785a7897417a006fc3694f3a1f781385471))
* **events:** add the sse event hub with ring buffer ([#4](https://github.com/OneLiteFeatherNET/grus/issues/4)) ([6822245](https://github.com/OneLiteFeatherNET/grus/commit/682224550e5a1dc03881caddd9d3bc3742abe5ea))
* **minestom:** read the velocity secret from a file in the demo lobby ([#54](https://github.com/OneLiteFeatherNET/grus/issues/54)) ([44129f0](https://github.com/OneLiteFeatherNET/grus/commit/44129f07688fba3d123401b646a0ae1e842b0af5))
* **sdk:** add the java sdk and minestom demo lobby ([#12](https://github.com/OneLiteFeatherNET/grus/issues/12)) ([f8ef69e](https://github.com/OneLiteFeatherNET/grus/commit/f8ef69e8a9e64e2600654b56a90833e04ba89c35))
* **store:** add the sqlite store with goose migrations ([#6](https://github.com/OneLiteFeatherNET/grus/issues/6)) ([d0ebece](https://github.com/OneLiteFeatherNET/grus/commit/d0ebecea1858c63358a6a8bd0ac9fd9631ef0447))
* **stub:** add a native example game server ([#42](https://github.com/OneLiteFeatherNET/grus/issues/42)) ([f52803b](https://github.com/OneLiteFeatherNET/grus/commit/f52803bdfd14f583b919e99ca925e6b7da5ad544))
* **templates:** add the content-addressed fs template store and fetch ([#7](https://github.com/OneLiteFeatherNET/grus/issues/7)) ([d380504](https://github.com/OneLiteFeatherNET/grus/commit/d380504a16a5a22275a291e3d17168fe712ce317))
* **templates:** store templates in s3 ([#44](https://github.com/OneLiteFeatherNET/grus/issues/44)) ([320c05a](https://github.com/OneLiteFeatherNET/grus/commit/320c05a755d21e73d0419aa53db3aa61eca28699))
* **velocity:** add the velocity plugin and proxy image ([#20](https://github.com/OneLiteFeatherNET/grus/issues/20)) ([4f5d516](https://github.com/OneLiteFeatherNET/grus/commit/4f5d5169e20f6e1f8f35a728e1f1f8d4de708b59))
* **velocity:** show the network-wide player count ([#43](https://github.com/OneLiteFeatherNET/grus/issues/43)) ([ad0369d](https://github.com/OneLiteFeatherNET/grus/commit/ad0369d3a874ff9e61e998e687202384fd44b709))
* **web:** add the instance pages ([#36](https://github.com/OneLiteFeatherNET/grus/issues/36)) ([2f5295f](https://github.com/OneLiteFeatherNET/grus/commit/2f5295f99a7bf69b51f39fabb6273b9d8f99f63e))
* **web:** add the live event pages ([#35](https://github.com/OneLiteFeatherNET/grus/issues/35)) ([49b16f8](https://github.com/OneLiteFeatherNET/grus/commit/49b16f8454cefbf62d488fae7858cf9b783b861f))
* **web:** add the nuxt web ui foundation ([#23](https://github.com/OneLiteFeatherNET/grus/issues/23)) ([3b8a39c](https://github.com/OneLiteFeatherNET/grus/commit/3b8a39c06ce0f20852afd1a3d32c1a9bb0443908))
* **web:** add the pool pages ([#37](https://github.com/OneLiteFeatherNET/grus/issues/37)) ([c5f3500](https://github.com/OneLiteFeatherNET/grus/commit/c5f3500f483890dfff5785296644717e338abc18))
* **web:** add the template page ([#34](https://github.com/OneLiteFeatherNET/grus/issues/34)) ([6acb93c](https://github.com/OneLiteFeatherNET/grus/commit/6acb93cde9150cc6a8ef63f0c72f356569ae7b73))


### Bug Fixes

* **chart:** mount a writable tmp directory for the controller ([#19](https://github.com/OneLiteFeatherNET/grus/issues/19)) ([cb52a91](https://github.com/OneLiteFeatherNET/grus/commit/cb52a91e20f822261615f96b68d8b01c2b78fcef))
* **cli:** show the host address and capacity in get hosts ([#58](https://github.com/OneLiteFeatherNET/grus/issues/58)) ([0e9433b](https://github.com/OneLiteFeatherNET/grus/commit/0e9433be5b12df993ba8b613f8619b337a01b248))
* **deps:** update dependency com.fasterxml.jackson:jackson-bom to v2.23.0 ([#15](https://github.com/OneLiteFeatherNET/grus/issues/15)) ([2ab7580](https://github.com/OneLiteFeatherNET/grus/commit/2ab758062300cb0e2c350b57fa6ae12c0d781268))
* **deps:** update dependency org.junit:junit-bom to v5.14.5 ([#59](https://github.com/OneLiteFeatherNET/grus/issues/59)) ([4f75912](https://github.com/OneLiteFeatherNET/grus/commit/4f75912f1e2124bc81f7b9687df5985fdd191e6a))
* **deps:** update patch updates ([#21](https://github.com/OneLiteFeatherNET/grus/issues/21)) ([48336d8](https://github.com/OneLiteFeatherNET/grus/commit/48336d8c1f3efedc5af4d47cd8344a82061439d9))
* **velocity:** keep the proxy install directory searchable for the runtime user ([#22](https://github.com/OneLiteFeatherNET/grus/issues/22)) ([0acb6b2](https://github.com/OneLiteFeatherNET/grus/commit/0acb6b2c3aa24ecfad09039ce2bdc26cbd027fa6))
* **web:** repair the pool page and keep api errors through ssr ([#38](https://github.com/OneLiteFeatherNET/grus/issues/38)) ([4839dd3](https://github.com/OneLiteFeatherNET/grus/commit/4839dd3c77dfd90c436a4f0bceabcc7e1128f2d7))
