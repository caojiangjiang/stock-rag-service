# Go模块代理配置
# 在项目目录创建 .netrc 文件，或设置环境变量

# 方法1: 设置环境变量（推荐）
export GOPROXY=https://goproxy.cn,https://goproxy.io,direct
export GOSUMDB=off

# 方法2: 修改 go.mod 所在目录的 GOPROXY
go env -w GOPROXY=https://goproxy.cn,https://goproxy.io,direct
go env -w GOSUMDB=off

# 方法3: 使用七牛云代理（国内最快）
go env -w GOPROXY=https://goproxy.cn,direct
