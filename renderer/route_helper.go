package renderer

import "html/template"

// RouteHelperViewData is the root template's view data key holding
// RouteHelperScript: put <script>{{ .escalatedRouteHelper }}</script> after
// the page's data script in the template passed to NewInertiaRenderer.
const RouteHelperViewData = "escalatedRouteHelper"

// RouteHelperScript installs a Ziggy-compatible window.route(name, params)
// that reads the `ziggy` page prop Escalated shares (the built-in route
// list). It does nothing when the host already has a window.route function
// (ziggy-js, or its own), and it reads window.Ziggy.routes first when the
// host sets one. An unknown name warns and returns "#".
const RouteHelperScript = `(function () {
  if (typeof window.route === 'function') return;
  function table() {
    try {
      var el = document.querySelector('script[data-page]');
      var page = el ? JSON.parse(el.textContent) : JSON.parse(document.getElementById('app').dataset.page);
      return (page.props && page.props.ziggy) || {};
    } catch (e) { return {}; }
  }
  var ziggy = table();
  window.route = function (name, params) {
    var routes = (window.Ziggy && window.Ziggy.routes) || ziggy.routes || {};
    var route = routes[name];
    if (!route) {
      if (window.console) console.warn('[escalated] no route named ' + name);
      return '#';
    }
    var list = params == null ? [] : (Array.isArray(params) ? params.slice() : (typeof params === 'object' ? null : [params]));
    var query = {};
    if (list === null) { for (var k in params) query[k] = params[k]; }
    var uri = route.uri.replace(/\{(\w+)\??\}/g, function (_, key) {
      var value;
      if (list !== null) { value = list.shift(); }
      else { value = query[key]; delete query[key]; }
      if (value != null && typeof value === 'object') value = value.reference != null ? value.reference : value.id;
      return value == null ? '' : encodeURIComponent(value);
    }).replace(/\/+$/, '');
    var search = Object.keys(query).filter(function (key) { return query[key] != null; })
      .map(function (key) { return encodeURIComponent(key) + '=' + encodeURIComponent(query[key]); }).join('&');
    return '/' + uri + (search ? '?' + search : '');
  };
})();`

// routeHelperJS is RouteHelperScript as trusted script for html/template.
var routeHelperJS = template.JS(RouteHelperScript)
