# windows inspect

对windows系统进行检查。  
需要在windows上运行一个http服务，web页面连接http服务执行相应PowerShell、bat命令获取windows系统信息，然后在web页面展示。  

## http服务
``` batch
:: 可选设置监听地址
:: set LISTEN_ADDR=0.0.0.0:5001
:: 可选设置日志级别
:: set LOG_LEVEL=INFO

:: 选择其一启动http服务
:: python
:: 安装依赖
:: pip install -r requirements.txt
python windows_execution.py

:: golang
:: 编译go代码
:: go build -o windows_execution.exe
windows_execution.exe
```

## web页面
```shell
# 安装依赖
npm install

# 启动项目
npm run dev

# 编译项目
npm run build
```