# buding-box-client 开发方案文档目录

## 目录用途

`E:\zxy\work-code\buding-box-client\dev-docs-buding` 是本项目后续产品重塑、品牌设计、兼容迁移和实施计划的优先文档目录。每个修改主题使用独立子目录；本轮主题为 `品牌修改`。代码、图片和构建产物仍放在原有工程目录中；Logo 源文件位于 `branding/assets/`，生成副本由同步脚本维护。

## 当前结论

本轮已在 `dev-zxy` 进入“品牌修改”实施完成后的审计阶段：品牌配置、临时 Logo、跨端读取层和主要用户可见入口已落地；运行时兼容标识仍按方案保留。Windows 386 主模块 Go 编译、关键回归、Desktop amd64 交叉构建、Web 和 Mobile 验证已完成；Browser 完整集成测试仍需可用 Chrome/DevTools 环境。

采用方向：

> 新品牌展示 + 新官网/安装包/系统元数据，保留旧 CLI、数据目录和协议兼容；未来预留完整身份迁移能力；About、许可证和原始归属信息必须真实。

## 文档清单

| 文档 | 用途 | 状态 |
|---|---|---|
| `E:\zxy\work-code\buding-box-client\dev-docs-buding\品牌修改\修改品牌方案.md` | 全仓库品牌审计原稿 | 基础审计 |
| `E:\zxy\work-code\buding-box-client\dev-docs-buding\品牌修改\品牌重塑设计方案.md` | 目标、边界、品牌架构和验收标准 | 已确认方向 |
| `E:\zxy\work-code\buding-box-client\dev-docs-buding\品牌修改\品牌资料与多语言文案占位稿.md` | Mock 团队、链接、色板和多语言资料 | Mock 待发布前替换 |
| `E:\zxy\work-code\buding-box-client\dev-docs-buding\品牌修改\品牌重塑实施计划.md` | 分阶段实施顺序和发布步骤 | 已执行 |
| `E:\zxy\work-code\buding-box-client\dev-docs-buding\品牌修改\完整身份迁移预留方案.md` | 未来 CLI、目录、协议、包标识和发布渠道迁移 | 预留方案 |
| `E:\zxy\work-code\buding-box-client\dev-docs-buding\品牌修改\品牌改造与Windows兼容性实施计划.md` | 本轮品牌与 Windows Go 验证执行计划 | 已执行 |
| `E:\zxy\work-code\buding-box-client\dev-docs-buding\品牌修改\品牌最终审计报告.md` | 分支、代码、构建、测试和残留风险最终审计 | 当前审计 |
| `E:\zxy\work-code\buding-box-client\dev-docs-buding\品牌修改\品牌与Windows兼容性验收清单.md` | 可勾选的交付验收清单和未来修改入口 | 当前验收 |

## 文档使用规则

1. 先阅读本文件，再阅读对应主题的设计方案，最后按实施计划执行。
2. 如果多个文档出现冲突，以该主题最新确认的设计方案为准；实施计划只能解释“怎么做”，不能改变产品决策。
3. 任何会影响旧用户数据、升级、配对、脚本或系统安装的改动，必须先补充迁移预留方案并完成兼容性评审。
4. 方案中的绝对路径以 `E:\zxy\work-code\buding-box-client` 为根，避免不同工作目录下产生歧义。
5. 代码修改前必须创建独立分支、记录当前版本、保留回滚点，并先完成阶段 0 的品牌资料确认。
6. 品牌名称、英文名称、About、邮箱、域名、团队信息和色板优先编辑 `E:\zxy\work-code\buding-box-client\branding\brand.json`；多语言 Mock 说明位于 `品牌资料与多语言文案占位稿.md`。
7. 不允许通过删除许可证、版权、第三方归属或虚构团队关系来制造“完全原创”的假象。
8. 后续每个修改主题在 `dev-docs-buding` 下新建一个独立子目录，并在本文件登记计划、验收和审计文档。
