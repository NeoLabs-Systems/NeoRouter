/// Public discovery data. No provider accounts, credentials or conversation data.
class BenchmarkMetric {
  BenchmarkMetric(Map data)
    : id = data['id'] as String,
      sourceId = data['sourceId'] as String,
      metric = data['metric'] as String,
      value = (data['value'] as num).toDouble(),
      percentile = (data['percentile'] as num).toDouble(),
      peers = (data['peers'] as num).toInt();
  final String id, sourceId, metric;
  final double value, percentile;
  final int peers;
}

abstract interface class RankedBenchmark {
  String get id;
  String get name;
  double get score;
  bool get isFree;
}

class BenchmarkModel implements RankedBenchmark {
  BenchmarkModel(Map data)
    : id = data['id'] as String,
      name = data['name'] as String,
      capability = data['capability'] as String,
      score = (data['score'] as num).toDouble(),
      sourceCount = (data['sourceCount'] as num).toInt(),
      isFree = data['isFree'] == true,
      price = (data['price'] as num?)?.toDouble(),
      vendor = data['vendor'] as String?,
      scores = (data['scores'] as List)
          .map((row) => BenchmarkMetric(row as Map))
          .toList();
  @override
  final String id, name;
  final String capability;
  @override
  final double score;
  final int sourceCount;
  @override
  final bool isFree;
  final double? price;
  final String? vendor;
  final List<BenchmarkMetric> scores;
}

class ReplayBenchmarkResult implements RankedBenchmark {
  ReplayBenchmarkResult(this.data);

  final Map data;
  @override
  String get id => data['model'] as String;
  @override
  String get name => data['name'] as String? ?? id;
  @override
  double get score => (data['score'] as num).toDouble() * 100;
  @override
  bool get isFree => false;
  String get task => data['task'] as String;
  int get samples => data['samples'] as int;
}

class BenchmarkSource {
  BenchmarkSource(Map data)
    : id = data['id'] as String,
      name = data['name'] as String,
      url = Uri.parse(data['url'] as String),
      updatedAt = DateTime.tryParse('${data['updatedAt']}'),
      modelCount = (data['modelCount'] as num).toInt();
  final String id, name;
  final Uri url;
  final DateTime? updatedAt;
  final int modelCount;
}

class BenchmarkPageData {
  BenchmarkPageData(Map data)
    : models = (data['models'] as List)
          .map((row) => BenchmarkModel(row as Map))
          .toList(),
      comparison = (data['comparison'] as List)
          .map((row) => BenchmarkModel(row as Map))
          .toList(),
      sources = (data['sources'] as List)
          .map((row) => BenchmarkSource(row as Map))
          .toList(),
      capabilities = (data['capabilities'] as List).cast<String>(),
      total = (data['total'] as num).toInt();
  final List<BenchmarkModel> models, comparison;
  final List<BenchmarkSource> sources;
  final List<String> capabilities;
  final int total;
}
