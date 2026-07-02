IMAGE_NAME:=packfs/router-packfs

build:
	DOCKER_BUILDKIT=1 docker build --network=host \
		--build-arg PREFIX= --build-arg ARCH=amd64 \
		-f Dockerfile -t $(IMAGE_NAME) ../../../..

