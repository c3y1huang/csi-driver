FROM alpine

# Add util-linux to get a new version of losetup.
RUN apk add util-linux
COPY ./_output/hostpathplugin /hostpathplugin
ENTRYPOINT ["/hostpathplugin"]