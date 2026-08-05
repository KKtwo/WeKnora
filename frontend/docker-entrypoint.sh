#!/bin/sh

# 空变量与未设置都回退到默认值，避免浏览器和 Nginx 得到不同上限。
MAX_FILE_SIZE_MB=${MAX_FILE_SIZE_MB:-100}

# 生成运行时配置文件，注入环境变量到前端
cat > /usr/share/nginx/html/config.js << EOF
window.__RUNTIME_CONFIG__ = {
  MAX_FILE_SIZE_MB: ${MAX_FILE_SIZE_MB}
};
EOF

# JSON 内联附件使用 base64（约 4/3 膨胀）；再预留 1 MiB 给请求元数据，业务层仍按原始文件字节校验。
export MAX_FILE_SIZE=$(((MAX_FILE_SIZE_MB * 4 + 2) / 3 + 1))M
export APP_HOST=${APP_HOST:-app}
export APP_PORT=${APP_PORT:-8080}
export APP_SCHEME=${APP_SCHEME:-http}
envsubst '${MAX_FILE_SIZE} ${APP_HOST} ${APP_PORT} ${APP_SCHEME}' < /etc/nginx/templates/default.conf.template > /etc/nginx/conf.d/default.conf

# 启动 nginx
exec nginx -g 'daemon off;'
