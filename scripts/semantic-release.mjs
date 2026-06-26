import { appendFileSync } from "node:fs";
import semanticRelease from "semantic-release";

const result = await semanticRelease();
const lines = [];

if (result) {
  lines.push("released=true");
  lines.push(`version=${result.nextRelease.version}`);
  lines.push(`tag=${result.nextRelease.gitTag}`);
} else {
  lines.push("released=false");
}

for (const line of lines) {
  console.log(line);
}

if (process.env.GITHUB_OUTPUT) {
  appendFileSync(process.env.GITHUB_OUTPUT, `${lines.join("\n")}\n`);
}
