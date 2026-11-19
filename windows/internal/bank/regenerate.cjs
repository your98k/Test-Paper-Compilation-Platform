'use strict';

// Build-time only; the checked-in output is consumed by Go's embed package.
const fs = require('node:fs');
const path = require('node:path');
const { createHash } = require('node:crypto');
const { gzipSync } = require('node:zlib');
const Registry = require('../../../core/registry.js');
const Schema = require('../../../core/schema.js');
const RNG = require('../../../core/rng.js');
const Generators = require('../../../generators/index.js');

const attemptLimit = 192;
const legacyLevels = [0, 0, 1, 2, 2];
const primaryOral = new Set('intAddSub intMul intDiv intMix decAddSub decMul decDiv fraction fracMD simple'.split(' '));
const primaryEstimate = new Set('intAddSub intMul intDiv decAddSub decMul decDiv approx'.split(' '));
const primaryApp = new Set('intAddSub intMul intDiv intMix decAddSub decMul decDiv approx area percent ratio volume factor money time fraction fracMD equation plant simple'.split(' '));
const primaryError = new Set('intAddSub intMul intDiv decAddSub decMul decDiv simple equation area'.split(' '));
const optionLabel = /^[A-F][.．、]\s*/;
const squash = text => text.replace(/\s/g, '');

function forms(subject, stage, kp) {
  if (subject === 'math' && stage === 'primary') {
    // Avoid legacy fallback routes that silently substitute another topic.
    const out = [['fill', 48], ['choice', 30], ['judge', 20]];
    if (primaryOral.has(kp)) out.push(['oral', 24]);
    if (primaryEstimate.has(kp)) out.push(['estimate', 16]);
    if (primaryApp.has(kp)) out.push(['app', 26]);
    if (primaryError.has(kp)) out.push(['error', 16]);
    return out;
  }
  // E.g. 'translation' for English was an alias for the same fill-in core.
  // Give each substantive source form one label in the offline corpus.
  let base = 'fill';
  if (subject === 'math' && stage === 'junior' && kp.endsWith('.app')) base = 'app';
  if (subject === 'chinese' && kp.endsWith('.sentence')) base = 'short';
  return [[base, 48], ['choice', 30], ['judge', 20]];
}

function keys(q) {
  const prompt = squash(q.stem.replace(/_{3,}|（\s*）/g, '').replace(/（写出(?:计算|解答)过程）$/, ''));
  const options = (q.opts || []).map(o => squash(o.replace(optionLabel, '')));
  const answer = options.length ? options[q.answer.charCodeAt(0) - 65] : squash(q.answer);
  return [JSON.stringify([prompt, answer]), JSON.stringify([prompt, ...options.sort()])];
}

function prepare(raw, ctx, name) {
  if (!raw) return null;
  // The legacy adapter puts the exercise's incorrect working in analysis.
  // Copy it into the visible prompt so the question is self-contained.
  if (raw.type === 'error') {
    const split = raw.analysis.indexOf('\n【错解辨析】');
    if (split < 0) return null;
    raw.stem += '\n' + raw.analysis.slice(split + '\n【错解辨析】'.length).replace(/；/g, '\n');
    raw.analysis = raw.analysis.slice(0, split);
  }
  if (raw.type === 'judge' && /^[√×]/.test(raw.answer)) raw.answer = raw.answer[0];
  const norm = Schema.normalize({ ...raw, ...ctx, diff: ctx.diff });
  if (!norm.ok) return null;
  const { subject, stage, grade, term, kp, type, diff, stem, opts, answer, analysis } = norm.question;
  if (/(?:\bNaN\b|\bInfinity\b|\bundefined\b)/.test([stem, answer, analysis, ...(opts || [])].join('\n'))) return null;
  if (opts) {
    // Most original choices include A./B. labels. Keep the visible format,
    // but reject duplicate option text and inconsistent answer labels.
    const bodies = opts.map(o => squash(o.replace(optionLabel, '')));
    if (bodies.some(o => !o) || new Set(bodies).size !== bodies.length ||
        !/^[A-F]$/.test(answer) || answer.charCodeAt(0) - 65 >= opts.length) return null;
  }
  if (ctx.subject === 'math' && ctx.stage === 'primary' && kp === 'decAddSub' && type === 'app') {
    // Legacy exercise asks how much more the first book costs; its generator
    // sometimes makes the first book cheaper and computes an absolute value.
    const match = stem.match(/^一本故事书 ([\d.]+) 元，一本绘本 ([\d.]+) 元/);
    if (match && Number(match[1]) <= Number(match[2])) return null;
  }
  return { subject, stage, grade, term, kp, kpName: name, type, diff, stem, opts, answer, analysis };
}

function sample(gen, { subject, stage, grade, term, kp, chapter, name }) {
  const rows = [];
  for (let diff = 1; diff <= 5; diff++) {
    const seen = new Set();
    for (const [type, limit] of forms(subject, stage, kp)) {
      const rng = RNG.create(['win-bank-v1', subject, stage, grade, term, kp, diff, type].join(':'));
      const ctx = { subject, stage, grade, term, kp, chapter, type, diff, rng };
      const sourceDiff = subject === 'math' && stage === 'primary' ? legacyLevels[diff - 1] : diff;
      let found = 0;
      for (let i = 0; found < limit && i < attemptLimit; i++) {
        const q = prepare(gen.generate({ ...ctx, diff: sourceDiff }), ctx, name);
        if (!q) continue;
        const [content, layout] = keys(q);
        if (seen.has(content) || seen.has(layout)) continue;
        seen.add(content);
        seen.add(layout);
        const id = createHash('sha256').update(JSON.stringify([subject, stage, grade, term, kp, diff, content]))
          .digest('hex').slice(0, 24);
        rows.push({ id: 'offline-' + id, ...q });
        found++;
      }
    }
  }
  return rows;
}

function build() {
  const curriculum = [];
  const corpus = [];
  const stats = {};
  const originalRandom = Math.random;
  // Legacy primary templates do not accept a seed argument. Scope the shim
  // to the synchronous generation call and do not modify the web project.
  try {
    for (const { id: subject, stages } of Registry.subjects()) {
      const statsRow = stats[subject] = { topics: 0, offline: 0, questions: 0 };
      for (const [stage, data] of Object.entries(stages)) {
        const source = Generators.get(subject, stage);
        const gen = source && {
          generate(ctx) {
            Math.random = ctx.rng.float;
            return source.generate(ctx);
          }
        };
        for (const grade of Schema.STAGES[stage].grades) {
          for (const term of [1, 2]) {
            const topics = [];
            for (const kp of Registry.curriculum(subject, stage, grade, term)) {
              const info = data.kpMap[kp];
              const chapter = Object.values(data.chapters).find(ch =>
                ch.grade === grade && ch.term === term && ch.kps.includes(kp))?.id;
              if (process.env.BANK_PROGRESS) console.error(subject, stage, grade, term, kp);
              const questions = info.gen && gen && source.kps.includes(kp)
                ? sample(gen, { subject, stage, grade, term, kp, chapter, name: info.name }) : [];
              topics.push({ id: kp, name: info.name, offline: questions.length > 0 });
              statsRow.topics++;
              if (questions.length) statsRow.offline++;
              statsRow.questions += questions.length;
              for (const q of questions) corpus.push(JSON.stringify(q));
            }
            if (topics.length) curriculum.push({ subject, stage, grade, term, topics });
          }
        }
      }
    }
  } finally {
    Math.random = originalRandom;
  }
  if (!corpus.length) throw new Error('Empty offline corpus');
  return {
    files: {
      'curriculum.json': Buffer.from(JSON.stringify(curriculum) + '\n'),
      'questions.jsonl.gz': gzipSync(Buffer.from(corpus.join('\n') + '\n'), { level: 9 })
    },
    stats
  };
}

const check = process.argv.slice(2).includes('--check');
const { files, stats } = build();
for (const [name, bytes] of Object.entries(files)) {
  const target = path.join(__dirname, name);
  if (check) {
    if (!fs.existsSync(target) || !fs.readFileSync(target).equals(bytes)) {
      console.error(`${name} differs; regenerate with node ${path.relative(process.cwd(), __filename)}`);
      process.exitCode = 1;
    }
  } else {
    fs.writeFileSync(target, bytes);
  }
  console.log(`${name}: ${bytes.length} bytes`);
}
console.table(stats);
