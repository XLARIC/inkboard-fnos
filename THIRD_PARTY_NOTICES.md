# 第三方软件与数据

- 应用源码和自制图标：MIT。演示天气和硬件均为合成数据。
- Go 标准库及嵌入运行时：BSD 3-Clause，完整许可随发行包提供，见 [Go-BSD.txt](licenses/Go-BSD.txt)。
- 嵌入时区数据来自 Go time/tzdata 和 [IANA 数据库](https://www.iana.org/time-zones)，相关许可见 [tz LICENSE](https://data.iana.org/time-zones/tzdb/LICENSE)。
- 天气和城市搜索：[Open-Meteo](https://open-meteo.com/)。天气数据按 [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/)署名，页面保留来源。免费 API 为非商业服务，商业用途需核对 [条款](https://open-meteo.com/en/terms)。接口见 [官方文档](https://open-meteo.com/en/docs)。模型回顾不等同于实测观测。
- 二维码：[skip2/go-qrcode](https://github.com/skip2/go-qrcode)，MIT，许可见 [go-qrcode-MIT.txt](licenses/go-qrcode-MIT.txt)。二维码在主端生成，没有外部二维码服务。
- 可选地址查询使用管理员配置的 Nominatim 兼容服务。OpenStreetMap 地址数据按 ODbL 署名，见 [OpenStreetMap copyright](https://www.openstreetmap.org/copyright)。公共服务遵守 [Nominatim 使用政策](https://operations.osmfoundation.org/policies/nominatim/)，默认未配置公共接口。
- 可选 Google Maps 选点通过 [Maps URLs](https://developers.google.com/maps/documentation/urls/get-started) 打开外部网页，不嵌入地图、抓取页面或调用 Google Geocoding／Places API；链接／坐标由用户复制，导入在主端本地解析。Google Maps 名称和商标归 Google，网页使用遵守 Google 自身条款。
- fnpack 为官方独立构建工具，不随项目重新分发。fnOS 名称和商标归原权利人。
- smartctl、intel_gpu_top、nvidia-smi 为主机可选程序，未捆绑且未链接其库，分别遵守 smartmontools、intel-gpu-tools、NVIDIA 许可。缺失时报告原因。
- Docker 使用官方 Go／Alpine 基础镜像，系统组件遵守各自许可。

第三方服务的数据条件不因应用采用 MIT 而改变。
