IMAGE_NAME:=hub.cstcloud.cn/packfs/database

build:
	DOCKER_BUILDKIT=1 docker build --network=host -t $(IMAGE_NAME) .

build-arm64:
	DOCKER_BUILDKIT=1 \
	DOCKER_DEFAULT_PLATFORM=linux/arm64 \
	docker build --network=host \
	    --platform linux/arm64 \
		--build-arg PREFIX=arm64v8/ \
		-t $(IMAGE_NAME):arm64v8 .

push: build
	docker push $(IMAGE_NAME)
	date

clean:
	docker rmi $(IMAGE_NAME)
