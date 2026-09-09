import type { NextConfig } from "next";

const repositoryName = process.env.GITHUB_REPOSITORY?.split("/")[1];
const basePath = process.env.GITHUB_ACTIONS === "true" && repositoryName
  ? `/${repositoryName}`
  : "";

const nextConfig: NextConfig = {
  assetPrefix: basePath,
  basePath,
  images: {
    unoptimized: true,
  },
  output: "export",
  trailingSlash: true,
};

export default nextConfig;
