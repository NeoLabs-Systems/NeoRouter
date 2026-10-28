'use strict';

const { HttpError } = require('./error_handler');

function validate(schema, location = 'body') {
  return (req, _res, next) => {
    const parsed = schema.safeParse(req[location]);
    if (!parsed.success) {
      const first = parsed.error.issues[0];
      const path = first?.path?.length ? `${first.path.join('.')}: ` : '';
      const message = first?.message ? `${path}${first.message}` : 'The request is invalid.';
      return next(new HttpError(400, 'VALIDATION_ERROR', message, parsed.error.flatten()));
    }
    req[location] = parsed.data;
    return next();
  };
}

module.exports = { validate };
