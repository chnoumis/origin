package(default_visibility = ["//visibility:public"])

licenses(["notice"])

load("@io_bazel_rules_go//go:def.bzl")

filegroup(
    name = "package-srcs",
    srcs = glob(["**"]),
    tags = ["automanaged"],
    visibility = ["//visibility:private"],
)

filegroup(
    name = "all-srcs",
    srcs = [
        ":package-srcs",
        "//staging/src/k8s.io/apiserver/pkg/endpoints/discovery:all-srcs",
        "//staging/src/k8s.io/apiserver/pkg/endpoints/handlers:all-srcs",
    ],
    tags = ["automanaged"],
)
