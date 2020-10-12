[gee-rpc](https://geektutu.com/post/geerpc.html)学习笔记
模仿标准库net/rpc实现基于gob编码的rpc框架，包含功能：
1. gob编码
2. 服务注册
3. 注册中心
4. 服务发现

# 大纲

## codec：编解码器，用于json/gob编解码，
* 编解码通过Option协商，用json格式，包含MagicNumber和编解码方式：json/gob
* 实体编码格式是 header + argv + replyv，其中header包含serviceMethod调用的方法和seq调用序列号

## server

* 监听listener，建立连接，循环读
* 根据rcsv注册service，通过反射得到rcsv的struct、方法列表并存储
* service对应一个struct的对象，methodType对应struct的导出的函数
* 从conn中读option、header、argv，然后根据header找到对应的方法method和receiver，调用method得到reply，写入到conn中

## client

* call 定义一个调用过程，包含seq和codec等
* dial连接，建立conn，和server协商codec，维护客户端状态
* 客户端发起调用，创建call，用client调用，client写入序列化header和args参数，用channel等待服务端调用写回的结果

## http support

* client和server通过HTTP CONNECT方法建立连接，hajack tcp连接用于RPC调用
* server跑起http server，持有Server，监听http请求处理server的调用状态

## load balance
抽象接口Discovery，用于节点选择
* 节点刷新
* 节点获取，支持选择算法

## 服务注册与服务发现
角色：注册中心，服务端、客户端
关系：
* 注册中心维护服务节点
* 服务端向注册中心注册刷新节点
* 客户端从注册中心发现可用节点

### Registry 注册中心

* registry，跑一个HTTP Server，支持get/post方法，get方法获取所有注册的服务节点，post方法更新服务节点，节点信息带在header中
* 提供向registry发送心跳(注册)服务节点的方法
* 每跑起一个server，向registry中注册该服务地址

### GeeRegistryDiscovery 使用注册中心的服务发现
* 用注册中心初始化
* 持有服务节点，并定期从注册中心刷新服务节点
* 实现接口Discovery

### XClient，带服务发现的客户端

* 用服务发现Discovery初始化，可选择节点选择模式
* 发起调用时，用Discovery确定应该响应的服务节点地址
* 与服务节点建立连接（或复用之前缓存的连接），初始化Client，开始RPC调用
