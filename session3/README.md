## 推送镜像至docker官方镜像仓库

docker push 6lkm/httpserver:v1

## 通过docker命令本地启动httpserver

docker run -d -p 5432:5432 6lkm/httpserver:v1

## 通过nsenter进入容器查看IP配置

nsenter -t $pid -n ip a