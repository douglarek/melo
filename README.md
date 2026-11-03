# Melo

Melo 是用 Go 和 [MyGo](https://github.com/egoist/mygo) 实现的第三方 YouTube Music 原生桌面客户端，界面参考 [mygo-cloudmusic](https://github.com/egoist/mygo-cloudmusic)。主界面由 MyGo UI 原生 GPU 渲染，登录环节复用系统网页视图。AAC-LC 播放链路为纯 Go 实现，HE-AAC 与音乐视频使用 ffmpeg。

## 功能特性

- **原生桌面体验**：深色 / 浅色 / 跟随系统外观，启动时恢复播放队列和进度。
- **原生通知**：切歌时在窗口失焦后显示封面、歌名和歌手。
- **托盘支持**：菜单栏 / 系统托盘可显示或隐藏窗口，并控制播放。
- **账号与媒体库**：应用内 Google 登录，同步「我喜欢的音乐」、云端歌单和点赞状态。
- **浏览与搜索**：首页、探索、排行榜、播客和搜索，支持歌单、专辑、艺人详情与分类筛选。
- **播放**：AAC-LC 纯 Go 解码，MP4 / HLS 流式播放，支持本地缓存、随机 / 循环和音量淡入淡出；HE-AAC 与音乐视频需要 `ffmpeg`。
- **正在播放**：大封面、同步歌词、待播清单、相关推荐和音乐视频。

## 构建与运行

需要 Go 1.27+。

```bash
# ffmpeg 可选：HE-AAC 曲目和音乐视频需要
brew install ffmpeg            # macOS
sudo apt install ffmpeg        # Debian / Ubuntu

# Linux 必选依赖
sudo apt install libwebkit2gtk-4.1-0 libayatana-appindicator3-1 \
  libgl1-mesa-dri libglx-mesa0 libegl-mesa0

# 测试
go test ./...

# 编译
go build .

# 开发模式
go tool mygo dev
```

## 致谢

- Gemini-3.8-flash & DeepSeek-V4.1-Flash-1017：氛围编码
- [MyGo](https://github.com/egoist/mygo)：原生 UI 与桌面应用框架
- [mygo-cloudmusic](https://github.com/egoist/mygo-cloudmusic)：界面结构与设计参考
- [go-aac](https://github.com/tphakala/go-aac)：纯 Go AAC-LC 解码实现

## 隐私

Melo 不包含遥测，也不会把本地数据上传给项目方。与 YouTube Music、LRCLIB 的网络请求仅用于登录、浏览、播放和歌词功能；登录 Cookie、播放记录、媒体库快照和缓存保存在本机用户数据目录。

## 免责声明

Melo 是非官方第三方客户端，与 YouTube、Google 没有关联，也未获得其认可或赞助。YouTube Music 及相关商标归其各自所有者。使用本软件访问内容时，请遵守 YouTube 的服务条款和当地法律；本项目不附带、不托管任何媒体内容。

## License

Melo 以 GNU Affero General Public License 3.0（AGPL-3.0）发布，详见 [LICENSE](LICENSE)。如果修改后的版本通过网络向用户提供服务，需要按 AGPL 第 13 条提供完整源码。分发包还包含采用其他许可证的第三方组件，包括 LGPL 的 `go-aac`；完整清单、许可证文本和源码获取方式见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
