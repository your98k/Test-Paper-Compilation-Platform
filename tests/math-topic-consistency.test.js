'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const bank = require('../utils/bank.js');
const Paper = require('../core/paper.js');

function verticalQuestion(kp) {
  bank.setContext(5, null);
  return bank.buildQuestion(kp, 'vertical', 3);
}

test('竖式题抽到简易方程时仍生成方程', () => {
  const q = verticalQuestion('equation');
  assert.match(q.q, /x/);
  assert.match(q.q, /=/);
  assert.doesNotMatch(q.q, /^\d+\s*[+−-]\s*\d+$/);
});

test('竖式题抽到多边形面积时仍生成面积题', () => {
  const q = verticalQuestion('area');
  assert.match(q.q, /面积/);
  assert.match(q.a, /平方/);
});

test('组卷时所选方程和面积不会退化成整数加减法', () => {
  const paper = Paper.generatePaper({
    subject: 'math', stage: 'primary', grade: 5, term: 1,
    kps: ['equation', 'area'], seed: 'topic-consistency',
    sections: [{ type: 'vertical', count: 12, score: 4 }]
  });
  assert.equal(paper.questions.length, 12);
  for (const q of paper.questions) {
    if (q.kp === 'equation') assert.match(q.stem, /x/);
    else if (q.kp === 'area') assert.match(q.stem, /面积/);
    else assert.fail(`生成了未选择的知识点：${q.kp}`);
  }
});
