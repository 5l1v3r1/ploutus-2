# Licensed Materials - Property of IBM
# © IBM Corp. 2019

TIMESTAMP := $(shell date +%s)
VERSION := v0.01-$(TIMESTAMP)



buildGo:
	mkdir -p out/app/$(VERSION)

	cd goApp/pkg/cmd ; env GOOS=linux go build -o ../../../out/app/$(VERSION)/ploutus-linux  -ldflags "-X main.version=$(VERSION)"
	cd goApp/pkg/cmd ; env GOOS=windows go build -o ../../../out/app/$(VERSION)/ploutus-windows  -ldflags "-X main.version=$(VERSION)"
	cd goApp/pkg/cmd ; env GOOS=darwin  go build -o ../../../out/app/$(VERSION)/ploutus-osx -ldflags "-X  main.version=$(VERSION)"
	rm -rf out/app/latest
	cd out/app ; ln  -s $(VERSION) latest


buildContainer:
	cp out/app/latest/ploutus-linux Docker/app/
	cd Docker ; docker build -t cminion/ploutus:$(VERSION) .
	cd Docker ; docker tag cminion/ploutus:$(VERSION) cminion/ploutus:latest 
