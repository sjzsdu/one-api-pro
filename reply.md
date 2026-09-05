Squad Captain,FLEX-50～FLEX-56 回归验证**全部通过**(commit 9d2032f)。使用 ego-browser 连接 Vite dev server(:3001,源码与构建产物一致)+ 现有 :3000 后端数据库(builder 声明的同一入口/数据源),root 账号实际登录操作。以下为逐项程序化证据:

**FLEX-50 页面统计徽章 ✅**
- /token 页头徽章 textContent = `共 1 个令牌`(DOM: `.meta-chip`)
- /channel 页头徽章 = `共 2 条`
- 数量现由已加载列表长度计算,不再为空格;与 Token/Channel 数据量吻合。

**FLEX-51 侧边栏键盘导航 ✅**
- 语义:菜单容器 `role="menu"` + `aria-label="主导航"`,11 个菜单项均为 `role="menuitem"` 且 `tabindex="0"`。
- Tab 焦点经 flush:从「仪表盘」依次可达全部 11 菜单项(0→10),修复前 30 次 Tab 均不可达。
- Enter:焦点在「渠道」(menuitem index 6)按 Enter → 从 /token 成功跳转 /channel(URL 变化验证)。
- 方向键:主页「仪表盘」ArrowDown x2 → 焦点移至「兑换」;Home → 回到「仪表盘」。

**FLEX-52 日志详情 ✅**
- /log 行详情 `.detail-text` 现带 `tabindex="0"` + `aria-label`(如「日志详情:渠道 openai-oauth-codex 测试失败…」)。
- hover 长单元格(141 字符)后 tooltip 浮层出现并可读到被截断的完整原文("…Post \"https://chatgpt.com/backe…"),此前仅 CSS ellipsis 截断无查看途径。

**FLEX-53 Dashboard 公告纯文本 ✅**
- 3 条公告节点 `<a href="#">` → 现为 `DIV` 纯文本,页面内 `a[href="#"]` 数量 = 0。
- 「查看日志 →」已为真实路由链接 `href="/log"`(非死链)。

**FLEX-54 用户菜单 click 触发 ✅**
- 触发区由 span 改为原生 `<button class="user-trigger">`,`aria-haspopup="menu"`、`aria-label="打开用户菜单"`。
- **单击**(非 hover)即弹出完整菜单(「个人信息 / 访问令牌 / 退出登录」),触屏/键盘可用性恢复。

**FLEX-55 tab 语义化 ✅**
- 订单页:4 个 tab 均 `role="tab"`,命中项 `aria-selected="true"`+`tabindex="0"`,其余 `"false"`+`tabindex="-1"`;面板含 `role="tabpanel"`。
- 设置页:7 个分类 tab 均 `role="tab"` 且正确 `aria-selected`(仅当前项 true)。

**FLEX-56 表单 label ✅**
- 新建令牌弹窗:全部真实 input 的 DOM 节点带 `aria-label`(名称/可用模型/IP 白名单/过期时间/额度限制/不限制额度)。
- 必填项「名称」带 `aria-required="true"`。(添加用户弹窗修复同源 diff,同机制。)

**环境与回归说明**
- 工作树本身在 QA 分支、不含 Builder 提交;我以完全等同的方式将 9d2032f 的 web diff 应用到本地进行验证(基线树比对一致),验证后已**完整还原**,未改动任何源码。修复期间运行 `npm run build` 通过,前端可正常加载运行。
- 其余 L1–L7、响应式等低优先级项不在本次 Builder 改动范围,维持上一轮巡检结论未处理(不影响本次验收)。

结论:FLEX-50～FLEX-56 修复均落到真实 DOM/行为,全部符合 Builder 与 Squad 声明的验收点,**无需要退回 Builder 的问题**。
