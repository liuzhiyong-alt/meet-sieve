# 声纹匹配校准记录

- Profile ID：`campplus-1.0.0-ms1-20260806-internal-2spk`
- 清单：`.local/voice-calibration/20260806/manifest.json`
- 模型：`iic/speech_campplus_sv_zh-cn_16k-common@1.0.0-ms1`
- 模型 SHA-256：`57f6b2439b06fc453ed36159a44b97693610fb0a67c0dafd696d54e24d2b1ae1`
- 说话人数：2
- 录入/评估样本：2 / 4
- 成员识别：正确 4，误认 0，拒识 0
- 匿名聚类：误合并 0，误拆分 0
- 成员阈值：min_score=0.700000，min_margin=0.100000
- 聚类阈值：min_score=0.650000，min_margin=0.080000

## 样本审计

| speaker_id | session_id | role | duration_ms | sha256 | top | score | matched |
| --- | --- | --- | ---: | --- | --- | ---: | --- |
| speaker-a | speaker-a-enrollment-20260805 | enrollment | 60000 | `a0465d6cc97947f8019a66f1f2450ff22a179f54f74f52cc904f07d7f419e4bc` |  | 0.000000 | false |
| speaker-a | speaker-a-evaluation-20260806-1 | evaluation | 60000 | `0fa18dee62db8308368ef68bfaaa80251a199e2f9bbb85f9a4dd323dd316224d` | speaker-a | 0.824062 | true |
| speaker-a | speaker-a-evaluation-20260806-2 | evaluation | 60000 | `0aa9f9974d052fc56eaa6f9f129831ae13bdf117af5053516d74b662b6fef428` | speaker-a | 0.862193 | true |
| speaker-b | speaker-b-enrollment-20260806-1 | enrollment | 27946 | `0de15f63a0ec777cf9dbadc8e433d0b25bd03383671c7b218fd2ebc70327cb8a` |  | 0.000000 | false |
| speaker-b | speaker-b-evaluation-20260806-2 | evaluation | 49813 | `a101041720c1f4c8e56804f102e9fe17cb2274590301dcdd2431005d7ed156df` | speaker-b | 0.867205 | true |
| speaker-b | speaker-b-evaluation-20260806-3 | evaluation | 51157 | `f22a702ea3adcc5fdd7690faa7b5312cf723c5f5880444e5e2965ae634880823` | speaker-b | 0.873495 | true |
