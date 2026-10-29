'use strict';

const { getDatabase } = require('../../db/database');

function record(action, { actorUserId, subjectUserId = null, detail = {} }) {
  getDatabase().prepare('INSERT INTO team_audit (action, actor_user_id, subject_user_id, detail_json) VALUES (?, ?, ?, ?)')
    .run(action, actorUserId, subjectUserId, JSON.stringify(detail));
}

function list(limit = 100) {
  return getDatabase().prepare(`SELECT a.*, actor.username actor_name, subject.username subject_name FROM team_audit a
    LEFT JOIN users actor ON actor.id = a.actor_user_id
    LEFT JOIN users subject ON subject.id = a.subject_user_id
    ORDER BY a.id DESC LIMIT ?`).all(Math.min(Math.max(Number(limit) || 100, 1), 500)).map((row) => ({
    id: row.id,
    action: row.action,
    actor: row.actor_name || null,
    subject: row.subject_name || null,
    detail: JSON.parse(row.detail_json || '{}'),
    createdAt: row.created_at,
  }));
}

module.exports = { record, list };
