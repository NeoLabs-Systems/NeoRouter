'use strict';

const { getDatabase } = require('./db/database');
const { createStatusHistory } = require('./status_history');
const history = createStatusHistory();

function buildPublicStatus() {
  let storage = false;
  let catalog = false;
  try {
    const db = getDatabase();
    db.prepare('SELECT 1').get();
    storage = true;
    db.prepare('SELECT 1 FROM models LIMIT 1').get();
    catalog = true;
  } catch { /* A failed check is shown as unavailable or degraded. */ }
  const components = [
    { group: 'Core', name: 'Web App & API', status: 'operational', description: 'The web server is responding to requests.' },
    { group: 'Core', name: 'Inference Gateway', status: storage ? 'operational' : 'degraded', description: storage ? 'Gateway routes are available. Individual providers are not tested.' : 'The gateway depends on unavailable storage.' },
    { group: 'Data', name: 'Storage', status: storage ? 'operational' : 'unavailable', description: storage ? 'The database is responding.' : 'The database could not be reached.' },
    { group: 'Routing', name: 'Model Catalog', status: catalog ? 'operational' : 'degraded', description: catalog ? 'The model catalog can be queried.' : 'The model catalog could not be queried.' },
  ];
  const status = components.some((item) => item.status === 'unavailable') ? 'unavailable' : components.some((item) => item.status === 'degraded') ? 'degraded' : 'operational';
  return { product: 'NeoRouter', status, updatedAt: new Date().toISOString(), refreshAfterSeconds: 60, note: status === 'operational' ? 'All monitored services are operational.' : 'One or more monitored services need attention.', components };
}
module.exports = { buildPublicStatus, history };
