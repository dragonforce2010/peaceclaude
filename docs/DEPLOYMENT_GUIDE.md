# PeaceClaude 独立官网托管与域名绑定指南（`peace-claude.app`）

`website/` 目录为纯静态零构建网站（无 Node/Webpack 构建依赖，开箱即部署），同时适配 **GitHub Pages**、**Vercel** 与 **Cloudflare Pages**，并可绑定自定义域名（如 `peace-claude.app`）。

> [!IMPORTANT]
> **私有仓库与 GitHub Pages 提醒**：当前主仓库 `dragonforce2010/peaceclaude` 保持 **Private**（私有）。GitHub 仅对 **GitHub Pro / Team / Enterprise** 或 **Public（公开）** 仓库开放免费 GitHub Pages 发布。如果你的账号为 GitHub Free 且不想公开主代码仓，**推荐使用 Vercel 或 Cloudflare Pages 直接部署 `website/` 目录，主仓库可永久保持 Private**！

---

## 方案 A：Vercel / Cloudflare Pages 部署（主仓可永久私有，推荐）

1. **导入仓库或目录**：
   - 在 [Vercel Dashboard](https://vercel.com/new) 中关联私有仓库 `dragonforce2010/peaceclaude`。
   - 将 **Root Directory** 设置为 `website`（无需 Build Command，Output Directory 留空或 `.`）。
   - `website/vercel.json` 已内置 `cleanUrls` 与静态安全响应头。
2. **绑定自定义域名 `peace-claude.app`**：
   - 注册域名（如 Porkbun / Cloudflare / Namecheap）后，在域名 DNS 面板按 Vercel 提示添加记录：
     - `A` 记录：`@` -> `76.76.21.21`
     - `CNAME` 记录：`www` -> `cname.vercel-dns.com.`
   - Vercel 会自动签发 HTTPS 证书并启用强制 HTTPS 重定向。
3. **如不想暴露主私有仓，也可单推静态展示仓**：
   - 只需新建一个仅含 `website/` 内容的公开静态仓库用于托管官网，主代码仓依旧私有。

---

## 方案 B：GitHub Pages 部署（适合 Pro 账户或官网拆出后使用）

1. 仓库已内置自动部署流水线 [`.github/workflows/pages.yml`](../.github/workflows/pages.yml)，推送修改到 `main` 分支的 `website/**` 时会自动触发上传与发布。
2. 在 GitHub 仓库 `Settings -> Pages -> Build and deployment` 中选择 **Source: GitHub Actions**。
3. 若绑定 `peace-claude.app`：
   - 将 [`website/CNAME.example`](../website/CNAME.example) 复制或重命名为 `website/CNAME`（确认内容只有一行 `peace-claude.app`）。
   - 在域名 DNS 添加 4 条 GitHub Pages `A` 记录：
     - `185.199.108.153`
     - `185.199.109.153`
     - `185.199.110.153`
     - `185.199.111.153`
   - 若使用 `www.peace-claude.app`，追加 `CNAME`: `www -> dragonforce2010.github.io.`。
   - 勾选 GitHub Pages 设置中的 **Enforce HTTPS**。

---

## 本地预览验证

```bash
python3 -m http.server 18991 -d website
# 浏览器打开 http://127.0.0.1:18991
```
