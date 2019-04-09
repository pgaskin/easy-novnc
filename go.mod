module github.com/geek1011/easy-novnc

go 1.12

require (
	github.com/gorilla/mux v1.7.1
	github.com/ogier/pflag v0.0.1
	github.com/shurcooL/httpfs v0.0.0-20181222201310-74dc9339e414 // indirect
	github.com/shurcooL/vfsgen v0.0.0-20181202132449-6a9ea43bcacd
	github.com/spkg/zipfs v0.0.0-20160624121328-4c5941d51e66
	golang.org/x/net v0.0.0-20190404232315-eb5bcb51f2a3
	golang.org/x/tools v0.0.0-20190407030857-0fdf0c73855b // indirect
)

replace github.com/spkg/zipfs => ./zipfs
