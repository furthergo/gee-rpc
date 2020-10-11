[gee-rpc](https://geektutu.com/post/geerpc.html)学习笔记
模仿标准库net/rpc实现基于gob编码的rpc框架，包含功能：
1. gob编码
2. 服务注册
3. 

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

## 