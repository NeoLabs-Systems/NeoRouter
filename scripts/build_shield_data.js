#!/usr/bin/env node
'use strict';

// Builds the word lists the privacy shield reads (server/services/processing/
// shield/data/*.txt) from public sources. Run it again to refresh them:
//   node scripts/build_shield_data.js
// The output is committed; the server never downloads anything.
const fs = require('node:fs');
const path = require('node:path');

const OUT = path.join(__dirname, '..', 'server', 'services', 'processing', 'shield', 'data');
const CENSUS = 'https://www2.census.gov/topics/genealogy/1990surnames';
const BERLIN = 'https://raw.githubusercontent.com/berlin/haeufige-vornamen-berlin/HEAD/data';
const BERLIN_YEARS = [2012, 2013, 2014, 2015, 2016, 2017, 2018, 2019, 2020, 2021, 2022, 2023];
const FREQUENCY = 'https://raw.githubusercontent.com/hermitdave/FrequencyWords/master/content/2018';
const BIP39 = 'https://raw.githubusercontent.com/bitcoin/bips/master/bip-0039/english.txt';

const LAST_NAMES_FROM_CENSUS = 15000;
const COMMON_WORDS_PER_LANGUAGE = 20000;
const FREQUENT_WORDS_PER_LANGUAGE = 600;

// First names of older generations, which the Berlin newborn lists miss.
const CLASSIC_GERMAN_FIRST_NAMES = `
Achim Adelheid Agnes Albert Alfons Alfred Alois Andrea Andreas Angelika Anita Anja Anke Annegret Anneliese Annemarie Antje Armin
Astrid Axel Barbara Bärbel Beate Bernd Bernhard Berta Bettina Birgit Björn Brigitte Bruno Carsten Christa Christel Christian Christiane
Christine Claudia Cornelia Dagmar Daniela Detlef Dieter Dietmar Dirk Doris Dorothea Edith Egon Elfriede Elke Elisabeth Elvira Emma Erich
Erika Erna Ernst Erwin Eugen Evelyn Ewald Franz Friedhelm Fritz Gabriele Georg Gerd Gerda Gerhard Gertrud Gisela Gottfried Gudrun Günter
Günther Hannelore Hans Hans-Jürgen Hans-Peter Harald Hartmut Hedwig Heide Heidi Heike Heinz Heinz-Dieter Helga Helmut Helmuth Herbert
Hermann Hildegard Holger Horst Hubert Ilse Ina Ingeborg Inge Ingo Ingrid Irene Irmgard Jens Joachim Jochen Jörg Josef Jürgen Jutta Karin
Karl-Heinz Katrin Klaus Kerstin Knut Konrad Kurt Liselotte Lothar Ludwig Manfred Manuela Margarete Margit Marianne Marion Marlies Martina
Mechthild Monika Norbert Olaf Ottmar Otto Petra Rainer Ralf Regina Reiner Reinhard Reinhold Renate Rita Roland Rolf Rosemarie Rudolf
Rüdiger Sabine Sieglinde Siegfried Silke Sonja Steffen Susanne Sven Thorsten Udo Ulf Ulla Ulrich Ulrike Ursula Ute Uwe Volker Waltraud
Werner Wilfried Wilhelm Willi Wolfgang Wolfram
`;

// Frequent family names in Germany (including Turkish, Polish and Italian
// ones), written as they are spelled here. Many are also first names or
// words ("Frank", "Koch"); being listed lets them count after a first name.
const GERMAN_LAST_NAMES = `
Müller Schmidt Schneider Fischer Weber Meyer Wagner Becker Schulz Hoffmann Schäfer Koch Bauer Richter Klein Wolf Schröder Neumann Schwarz
Zimmermann Braun Krüger Hofmann Hartmann Lange Schmitt Werner Schmitz Krause Meier Lehmann Schmid Schulze Maier Köhler Herrmann König Walter
Mayer Huber Kaiser Fuchs Peters Lang Scholz Möller Weiß Jung Hahn Schubert Vogel Friedrich Keller Günther Frank Berger Winkler Roth Beck
Lorenz Baumann Franke Albrecht Schuster Simon Ludwig Böhm Winter Kraus Martin Schumacher Krämer Vogt Stein Jäger Otto Sommer Groß Seidel
Heinrich Brandt Haas Schreiber Graf Schulte Dietrich Ziegler Kuhn Kühn Pohl Engel Horn Busch Bergmann Thomas Voigt Sauer Arnold Wolff
Pfeiffer Lindner Ernst Hübner Kramer Jansen Franz Peter Hammer Götz Hesse Nowak Kowalski Lewandowski Wójcik Kamiński Kowalczyk Zieliński
Szymański Woźniak Dąbrowski Yılmaz Kaya Demir Şahin Çelik Yıldız Yıldırım Öztürk Aydın Özdemir Arslan Doğan Kılıç Aslan Çetin Kara Koç Kurt
Özkan Şimşek Rossi Russo Ferrari Esposito Bianchi Romano Colombo Ricci Marino Greco Bruno Gallo Conti Costa Petrović Jovanović Popović
Ivanov Petrov Smirnov Nguyen Tran Le Pham Hoang Ahmadi Hosseini Mohammadi Haidari Weiss Gross Mueller Schaefer Kruger Kohler Konig Bohm
Boehm Kraemer Jaeger Voss Witt Brinkmann Kuhlmann Busse Thiel Ebert Schütz Schuetz Schwab Hein Heinz Seifert Ritter Hermann Wendt
Böttcher Kraft Schilling Hartwig Kunz Kühne Pieper Gerlach Marx Kirchner Ulrich Fiedler Vollmer Paul Rau Brand Maurer Wolter Stark
Arndt Bach Haase Sander Bock Kolb Behrens Barth Lohmann Ahrens Petersen Thomsen Hansen Christiansen Nielsen Jensen Andersen Carstensen
`;

// First names that are everyday words; they need a known last name after them.
const AMBIGUOUS_FIRST_NAMES = `
will mark rose may grace hope art bill frank ernst faith joy june august rich sunny summer autumn dawn glen guy jack jean ray rob sue pat
carol chase chance crystal destiny dean drew earl gene lane lee miles norman page penny ruby sandy sterling wade ward wes roman heide kai
rosa liebe sonne golf major king prince angel amber april ivy iris jade lily violet holly heather daisy hazel olive pearl ginger cherry
cliff dale forest hunter mason taylor tanner archer baker carter cooper fisher porter skip buck chip dusty rusty sky storm river rain
nova mercedes kia diamond star trinity harmony melody justice royal sage willow
`;

const words = (block) => block.split(/\s+/).filter(Boolean);

async function get(url) {
  const response = await fetch(url, { headers: { 'User-Agent': 'NeoRouter shield data builder' } });
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
  return response.text();
}

const titleCase = (name) => name.toLowerCase().replace(/(^|[-' ])(\p{L})/gu, (match, separator, letter) => separator + letter.toUpperCase());

function write(name, header, entries) {
  const sorted = [...new Set(entries)].sort((a, b) => a.localeCompare(b, 'de'));
  fs.writeFileSync(path.join(OUT, `${name}.txt`), `${header.map((line) => `# ${line}`).join('\n')}\n${sorted.join('\n')}\n`);
  console.log(`${name}.txt: ${sorted.length} entries`);
}

async function main() {
  fs.mkdirSync(OUT, { recursive: true });

  const censusNames = (text) => text.split('\n').map((line) => line.trim().split(/\s+/)[0]).filter(Boolean).map(titleCase);
  const usFirst = [...censusNames(await get(`${CENSUS}/dist.male.first`)), ...censusNames(await get(`${CENSUS}/dist.female.first`))];
  const berlinCounts = new Map();
  for (const year of BERLIN_YEARS) {
    const data = JSON.parse(await get(`${BERLIN}/${year}/all_names.json`));
    for (const district of Object.values(data.common_names)) {
      for (const [name, genders] of Object.entries(district)) {
        const count = Object.values(genders).flatMap((positions) => Object.values(positions)).reduce((sum, value) => sum + value, 0);
        berlinCounts.set(name, (berlinCounts.get(name) || 0) + count);
      }
    }
  }
  const berlinFirst = [...berlinCounts].filter(([name, count]) => count >= 3 && /^\p{Lu}\p{Ll}+(?:-\p{Lu}\p{Ll}+)?$/u.test(name)).map(([name]) => name);
  const ambiguous = new Set(words(AMBIGUOUS_FIRST_NAMES));
  const firstNames = [...usFirst, ...berlinFirst, ...words(CLASSIC_GERMAN_FIRST_NAMES)];
  const firstLower = new Set(firstNames.map((name) => name.toLowerCase()));
  write('first_names', [
    'First names. They stay in the text; the last name after them is hidden.',
    'Sources: US Census Bureau 1990 name files (public domain);',
    'BerlinOnline GmbH, auf Basis von Daten des Berliner Landesamtes für Bürger- und Ordnungsangelegenheiten (LABO),',
    '"Liste der häufigen Vornamen" 2012-2023, CC BY 3.0 DE (https://creativecommons.org/licenses/by/3.0/de/);',
    'classic German first names added by hand. Built by scripts/build_shield_data.js.',
  ], firstNames);

  const germanLast = words(GERMAN_LAST_NAMES);
  const censusLast = censusNames(await get(`${CENSUS}/dist.all.last`)).slice(0, LAST_NAMES_FROM_CENSUS)
    .filter((name) => !firstLower.has(name.toLowerCase()));
  write('last_names', [
    'Last names. Names that are also first names only come from the German list.',
    `Sources: US Census Bureau 1990 surname file, top ${LAST_NAMES_FROM_CENSUS} (public domain);`,
    'frequent family names in Germany added by hand. Built by scripts/build_shield_data.js.',
  ], [...censusLast, ...germanLast]);

  const lastLower = new Set([...censusLast, ...germanLast].map((name) => name.toLowerCase()));
  const common = [];
  const frequent = [];
  for (const language of ['de', 'en']) {
    const lines = (await get(`${FREQUENCY}/${language}/${language}_50k.txt`)).split('\n').slice(0, COMMON_WORDS_PER_LANGUAGE);
    for (const [rank, line] of lines.entries()) {
      const word = line.split(' ')[0];
      if (rank < FREQUENT_WORDS_PER_LANGUAGE && word) frequent.push(word);
      if (word && word.length >= 2 && /^\p{Ll}+$/u.test(word) && ((!firstLower.has(word) && !lastLower.has(word)) || ambiguous.has(word))) common.push(word);
    }
  }
  write('frequent_words', [
    'The most frequent German and English words, names included ("in", "so", "das", "you").',
    'A name written in lowercase or capitals is only trusted when it is none of these.',
    `Source: Hermit Dave, FrequencyWords (OpenSubtitles 2018), top ${FREQUENT_WORDS_PER_LANGUAGE} per language, CC BY-SA 4.0.`,
    'Built by scripts/build_shield_data.js.',
  ], frequent);
  write('common_words', [
    'Frequent German and English words. A capitalised one after a first name is no last name ("Max Bescheid").',
    `Source: Hermit Dave, FrequencyWords (OpenSubtitles 2018), top ${COMMON_WORDS_PER_LANGUAGE} per language,`,
    'CC BY-SA 4.0 (https://creativecommons.org/licenses/by-sa/4.0/); names removed, ambiguous first names kept.',
    'Built by scripts/build_shield_data.js.',
  ], common);

  const bip39 = (await get(BIP39)).split('\n').map((line) => line.trim()).filter(Boolean);
  if (bip39.length !== 2048) throw new Error(`BIP-39 wordlist has ${bip39.length} words, expected 2048.`);
  // Order matters (a word's position is its value), so this one is not sorted.
  fs.writeFileSync(path.join(OUT, 'bip39_english.txt'), `# BIP-39 English wordlist, in order (https://github.com/bitcoin/bips/blob/master/bip-0039/english.txt), MIT.\n${bip39.join('\n')}\n`);
  console.log('bip39_english.txt: 2048 entries');
}

main().catch((error) => {
  console.error(error.message);
  process.exitCode = 1;
});
