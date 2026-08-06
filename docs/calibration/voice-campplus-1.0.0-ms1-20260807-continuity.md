# CAM++ continuity 校准记录

- Profile ID：`campplus-1.0.0-ms1-20260807-continuity-2spk`
- 清单：`docs/calibration/continuity-campplus-1.0.0-ms1-20260807.json`
- 模型：`iic/speech_campplus_sv_zh-cn_16k-common@1.0.0-ms1`
- 模型 SHA-256：`57f6b2439b06fc453ed36159a44b97693610fb0a67c0dafd696d54e24d2b1ae1`
- 窗口 / hop：`3000 ms / 3000 ms`
- continuity 阈值：`min_score=0.47190093197308114`，`min_margin=0.21849440781520246`

## 阈值选择与独立验证

selection 集最近邻统计：同人最低 `0.6448056853488776`，异人最高
`0.2989961785972847`，最小同异人 margin 为 `0.4369888156304049`。工具使用固定规则：

1. `min_score = (selection_min_top_same + selection_max_top_different) / 2`；
2. `min_margin = selection_min_margin / 2`。

validation 集 9 条最近邻判断全部通过。validation 异人分数最高 `0.16040982614124075`；
按生产 provider-key scoped centroid 路由顺序重放得到 4 个 segment，误合并 0、误拆分 0。

## 短窗 PCM 审计

以下摘要仅覆盖实际进入 encoder 的 3 秒以内 PCM16 短窗，不包含音频、转写正文或姓名。

| sample | split | PCM SHA-256 |
| --- | --- | --- |
| sel-a-01 | selection | `6a4e75c1243e3eca28d2d3a86c5930c9adbe449e3e8f2b222c27f2b859385bc3` |
| sel-b-03 | selection | `1c87d52469ff067d1b13fb196c27bea5d824376aea6e76b05b194067aedcbceb` |
| val-b-04 | validation | `0ce34c5b7e7d4e126153da50b9e6e945394e6bf58609e74e112d1843460a818b` |
| val-b-06 | validation | `3976b322e4ca886a1ec2a86cd2999ffb041cf02f8a224a615708d0a9654ff907` |
| sel-b-07 | selection | `a556b9ae85a2829c783a7a0d8d3e26a958d69dd2565955c4ff568c8648c9e165` |
| sel-a-08 | selection | `2d738f2f240e887fb962f6e554af1bb8dcbd3bea3de04888385b2be2e04e22ff` |
| val-a-09 | validation | `3d784ac02a6c25694ac34017406c5a1449853e6029ba61e2fe8287e0cfbd8a52` |
| val-a-10 | validation | `50654df40811239460ae78a1f61b1e328869b151a7fbd2e054ad4d1283ad101b` |
| sel-b-12 | selection | `76ce503ee9b81e825bcae6b6fc94361010ad351691d408723fbaad3c8c6c2c2a` |
| sel-a-16 | selection | `d2dc454e48823ec76a1716cfee025358b26b710dea5642eef686d15cc532af1a` |
| sel-b-17 | selection | `1963755f5a7bd762ac53eaea05c1aa105d4503d617920b5e59b6b94305fae2b9` |
| val-b-18 | validation | `019abcc59b3115b16a1c6a532caaf5cda923b4e9c3582da86c7cf5c0ac2adc72` |
| val-b-19 | validation | `6ff6b14bf5cbf2b4d7c81bae69e8fdc197c9a0c5111cc5ff6bd265b91b0a6946` |
| val-b-21 | validation | `a8943b3071453bb06f05ab1cc9553b1723a34383f760ae447a3723978a525297` |
| val-b-22 | validation | `c24956c9095a9ef1c169b55155eec3bbcf1b3e64d6576d724ee98c5016ca5938` |
| val-b-23 | validation | `be4baa74dee835b29db5dbe8f9b30e9d270bea71d27066372f6811708bc788e7` |

## 复现命令

使用数据库副本运行，禁止直接修改现场数据库：

```text
go run ./buildtools/cmd/analyzecontinuity \
  -manifest docs/calibration/continuity-campplus-1.0.0-ms1-20260807.json \
  -database <meetings.db 副本> -workspace <会议工作目录> \
  -model <锁定 model.onnx> -runtime <锁定 ONNX Runtime>
```
