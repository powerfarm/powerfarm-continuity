#!/usr/bin/env node
"use strict";

const { execFileSync } = require("node:child_process");
const { PromisesApi, CKEnvironment, CKDatabaseType } = require("@apple/cktool.database");
const { createConfiguration } = require("@apple/cktool.target.nodejs");

const CONTAINER_ID = process.env.POWERFARM_CLOUDKIT_CONTAINER || "iCloud.app.powerfarm";
const zoneName = process.argv[2];
const environmentName = (process.env.POWERFARM_CLOUDKIT_ENVIRONMENT || "development").toLowerCase();

if (!zoneName || !/^pf\.[a-z0-9][a-z0-9.-]*$/.test(zoneName)) {
  console.error("usage: provision-private-zone.js pf.<namespace>");
  process.exit(64);
}

const environment = environmentName === "production"
  ? CKEnvironment.PRODUCTION
  : CKEnvironment.DEVELOPMENT;

function userToken() {
  if (process.env.CLOUDKIT_USER_TOKEN) return process.env.CLOUDKIT_USER_TOKEN;
  try {
    return execFileSync(
      "/usr/bin/security",
      ["find-generic-password", "-s", "com.apple.icloud.cktool", "-a", "cktooluser_auth", "-w"],
      { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }
    ).trim();
  } catch {
    console.error("cloudkit_user_token=missing");
    console.error("action=xcrun cktool save-token --type user");
    process.exit(78);
  }
}

function safeError(error) {
  const status = error && (error.statusCode || error.status || error.code);
  const name = error && error.constructor ? error.constructor.name : "Error";
  return { name, status: status ?? "unknown" };
}

async function main() {
  const api = new PromisesApi({
    configuration: createConfiguration(),
    security: { UserTokenAuth: userToken() }
  });

  const common = {
    containerId: CONTAINER_ID,
    environment,
    databaseType: CKDatabaseType.PRIVATE
  };

  try {
    const response = await api.createZone({
      ...common,
      body: { zoneName, zoneType: "REGULAR_CUSTOM_ZONE" }
    });
    const zone = response.result && response.result.zone;
    console.log("result=created");
    console.log(`container=${CONTAINER_ID}`);
    console.log("database=private");
    console.log(`environment=${environmentName}`);
    console.log(`zone=${zone?.zoneName || zoneName}`);
  } catch (createError) {
    try {
      const response = await api.getZone({ ...common, zoneName });
      const zone = response.result && response.result.zone;
      console.log("result=exists");
      console.log(`container=${CONTAINER_ID}`);
      console.log("database=private");
      console.log(`environment=${environmentName}`);
      console.log(`zone=${zone?.zoneName || zoneName}`);
    } catch (getError) {
      const c = safeError(createError);
      const g = safeError(getError);
      console.error(`create_error=${c.name}:${c.status}`);
      console.error(`verify_error=${g.name}:${g.status}`);
      process.exit(1);
    }
  }
}

main();
