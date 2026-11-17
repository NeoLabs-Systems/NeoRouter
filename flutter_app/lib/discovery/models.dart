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

class BenchmarkModel {
  BenchmarkModel(Map data)
    : id = data['id'] as String,
      name = data['name'] as String,
      capability = data['capability'] as String,
      score = (data['score'] as num).toDouble(),
      sourceCount = (data['sourceCount'] as num).toInt(),
      isFree = data['isFree'] == true,
      scores = (data['scores'] as List)
          .map((row) => BenchmarkMetric(row as Map))
          .toList();
  final String id, name, capability;
  final double score;
  final int sourceCount;
  final bool isFree;
  final List<BenchmarkMetric> scores;
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

class ModelNews {
  ModelNews(Map data)
    : id = data['id'] as String,
      name = data['name'] as String,
      modelId = data['modelId'] as String,
      score = (data['score'] as num).toDouble(),
      sourceCount = (data['sourceCount'] as num).toInt(),
      contextWindow = (data['contextWindow'] as num?)?.toInt(),
      detectedAt = DateTime.fromMillisecondsSinceEpoch(
        (data['detectedAt'] as num).toInt(),
      ),
      lastSeenAt = DateTime.fromMillisecondsSinceEpoch(
        (data['lastSeenAt'] as num).toInt(),
      ),
      available = data['available'] == true,
      url = Uri.parse(data['url'] as String);
  final String id, name, modelId;
  final double score;
  final int sourceCount;
  final int? contextWindow;
  final DateTime detectedAt, lastSeenAt;
  final bool available;
  final Uri url;
}

class ModelNewsPage {
  ModelNewsPage(Map data)
    : items = (data['items'] as List)
          .map((row) => ModelNews(row as Map))
          .toList(),
      total = (data['total'] as num).toInt(),
      asOf = (data['asOf'] as num).toInt(),
      updatedAt = DateTime.tryParse('${data['updatedAt']}');
  final List<ModelNews> items;
  final int total, asOf;
  final DateTime? updatedAt;
}
