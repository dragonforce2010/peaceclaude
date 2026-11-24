# PeaceClaude 独立官网托管与域名绑定指南（`peace-claude.app`）

`website/` 目录为纯静态零构建网站（无 Node/Webpack 构建依赖，开箱即部署），同时适配 **GitHub Pages**、**Vercel** 与 **Cloudflare Pages**，并可绑定自定义域名（如 `peace-claude.app`）。

> [!NOTE]
> **当前正式官网托管方式**：本仓库 `dragonforce2010/peaceclaude` 现已公开，官网通过 GitHub Actions 工作流 [`.github/workflows/pages.yml`](../.github/workflows/pages.yml) 自动发布至 **GitHub Pages**：`https://dragonforce2010.github.io/peaceclaude/`。推送至 `main` 分支中 `website/**` 的改动会自动部署。

---

## 方案 A：GitHub Pages 部署（当前线上正式主站）

1. 仓库已内置自动部署流水线 [`.github/workflows/pages.yml`](../.github/workflows/pages.yml)，推送修改到 `main` 分支的 `website/**` 时会自动触发上传与发布。
2. GitHub 仓库 `Settings -> Pages -> Build and deployment` 已配置为 **Source: GitHub Actions**，默认访问地址为 `https://dragonforce2010.github.io/peaceclaude/`。
3. 若绑定自定义域名 `peace-claude.app`：
   - 将 [`website/CNAME.example`](../website/CNAME.example) 复制或重命名为 `website/CNAME`（确认内容只有一行 `peace-claude.app`）。
   - 在域名 DNS 添加 4 条 GitHub Pages `A` 记录：
     - `185.199.108.153`
     - `185.199.109.153`
     - `185.199.110.153`
     - `185.199.111.153`
   - 若使用 `www.peace-claude.app`，追加 `CNAME`: `www -> dragonforce2010.github.io.`。
   - 在 GitHub Pages 设置中勾选 **Enforce HTTPS**。

---

## 方案 B：Vercel / Cloudflare Pages 备选托管

1. 在 [Vercel Dashboard](https://vercel.com/new) 中关联仓库 `dragonforce2010/peaceclaude`。
2. 将 **Root Directory** 设置为 `website`（无需 Build Command，Output Directory 留空或 `.`）。
3. `website/vercel.json` 已内置 `cleanUrls` 与静态安全响应头，绑定 `peace-claude.app` 后自动签发 HTTPS 证书。

---

## 方案 C：Netlify CLI 备选托管

曾使用的备用预览站点：[https://peaceclaude-hub.netlify.app](https://peaceclaude-hub.netlify.app)（站点 ID `0444afef-3945-49ef-ba7c-dd0068ff5a36`）。如需同步到 Netlify：

```bash
cd /tmp && netlify deploy --prod \
  --dir=/path/to/peaceclaude/website \
  --site=0444afef-3945-49ef-ba7c-dd0068ff5a36 \
  --message="Update PeaceClaude website"
```

绑定自定义域名 `peace-claude.app`：打开 [Netlify 项目域名设置](https://app.netlify.com/projects/peaceclaude-hub) -> `Domain management` -> `Add a domain` 填入 `peace-claude.app`，按页面提供的记录到 DNS 服务商添加即可自动签发 Let's Encrypt HTTPS 证书。

---

## 本地预览验证

```bash
python3 -m http.server 18991 -d website
# 浏览器打开 http://127.0.0.1:18991
```
