define([
  'angular',
  'lodash',
  'kbn',
  'moment'
],
function (angular, _, kbn) {
  'use strict';

  var module = angular.module('grafana.services');

  module.factory('CustomDatasource', function($q, $http) {

    // the datasource object passed to constructor
    // is the same defined in config.js
    function CustomDatasource(datasource) {
      this.name = datasource.name;
      this.supportMetrics = false;
      this.supportAnnotations = true;
      this.url = datasource.url;
      this.grafanaDB = datasource.grafanaDB;
      console.log(this);
    };

    CustomDatasource.prototype.annotationQuery = function(annotation, rangeUnparsed) {
        console.log("annotationQuery")
        return [];
    };

    CustomDatasource.prototype.query = function(options) {
      console.log(options);
      return [];
    };

    CustomDatasource.prototype.deleteDashboard = function(id) {
      console.log("deleteDashboard "+id)
      var deferred = $q.defer()

        $http.delete('http://localhost:8080/dashboard/'+id).
          success(function(data, status, headers, config) {
            deferred.resolve(id);
          }).
          error(function(data, status, headers, config) {
            deferred.reject("Unable to delete "+id);
          });

      return deferred.promise;
    };

    CustomDatasource.prototype.searchDashboards = function(queryString) {
        var deferred = $q.defer()
        $http.get('http://localhost:8080/search?query='+queryString).
          success(function(data, status, headers, config) {
            deferred.resolve(data);
          }).
          error(function(data, status, headers, config) {
            deferred.reject("Unable to search");
          });
          return deferred.promise;
    };

    CustomDatasource.prototype.getDashboard = function(id, isTemp) {
        var deferred = $q.defer()
        $http.get('http://localhost:8080/dashboard/'+id).
          success(function(data, status, headers, config) {
            deferred.resolve(data);
          }).
          error(function(data, status, headers, config) {
            deferred.reject("Unable to get dashboard "+id);
          });

          return deferred.promise;
    };

    CustomDatasource.prototype.saveDashboard = function(dashboard) {
        var id = encodeURIComponent(kbn.slugifyForUrl(dashboard.title));
        dashboard.id = id;

        var deferred = $q.defer()
        $http.post('http://localhost:8080/dashboard/'+id, dashboard).
          success(function(data, status, headers, config) {
            deferred.resolve({ title: dashboard.title, url: "/dashboard/db/" + id })
          }).
          error(function(data, status, headers, config) {
            deferred.reject("Unabled to save dashboard "+id);
          });

         return deferred.promise;
    };

    return CustomDatasource;

  });

});